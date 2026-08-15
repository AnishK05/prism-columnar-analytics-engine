package kernel

import (
	"container/heap"
	"fmt"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// OrderKey is one ORDER BY item (Postgres-style: NULLS LAST for ASC).
type OrderKey struct {
	Name string
	Desc bool
}

// SortRecord returns a new record ordered by keys. Caller Releases the result.
func SortRecord(rec arrow.Record, keys []OrderKey) (arrow.Record, error) {
	n := int(rec.NumRows())
	if n <= 1 || len(keys) == 0 {
		rec.Retain()
		return rec, nil
	}
	cols, cloned, err := orderArrays(rec, keys)
	if err != nil {
		return nil, err
	}
	defer releaseAll(cloned)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return cmpRows(cols, keys, idx[i], idx[j]) < 0
	})
	return permute(rec, idx)
}

func orderArrays(rec arrow.Record, keys []OrderKey) ([]arrow.Array, []arrow.Array, error) {
	cols := make([]arrow.Array, len(keys))
	var cloned []arrow.Array
	for i, k := range keys {
		arr, err := colByName(rec, k.Name)
		if err != nil {
			return nil, nil, err
		}
		var extra bool
		arr, extra = unwrapDict(arr)
		if extra {
			cloned = append(cloned, arr)
		}
		cols[i] = arr
	}
	return cols, cloned, nil
}

func cmpRows(cols []arrow.Array, keys []OrderKey, i, j int) int {
	for c, k := range keys {
		a, _ := cellFromArray(cols[c], i)
		b, _ := cellFromArray(cols[c], j)
		cmp := cmpCell(a, b)
		if cmp == 0 {
			continue
		}
		if k.Desc {
			return -cmp
		}
		return cmp
	}
	return 0
}

// EmptyRecord is a 0-row record with the given schema.
func EmptyRecord(schema *arrow.Schema) arrow.Record {
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	return b.NewRecord()
}

func permute(rec arrow.Record, idx []int) (arrow.Record, error) {
	cols := make([]arrow.Array, rec.NumCols())
	for c := 0; c < int(rec.NumCols()); c++ {
		taken, err := takeArray(rec.Column(c), idx)
		if err != nil {
			for _, col := range cols {
				if col != nil {
					col.Release()
				}
			}
			return nil, err
		}
		cols[c] = taken
	}
	return array.NewRecord(rec.Schema(), cols, int64(len(idx))), nil
}

// LimitRecord keeps the first n rows. n < 0 is treated as 0.
func LimitRecord(rec arrow.Record, n int64) (arrow.Record, error) {
	if n < 0 {
		n = 0
	}
	if rec.NumRows() <= n {
		rec.Retain()
		return rec, nil
	}
	idx := make([]int, int(n))
	for i := range idx {
		idx[i] = i
	}
	return permute(rec, idx)
}

func releaseAll(arrs []arrow.Array) {
	for _, a := range arrs {
		if a != nil {
			a.Release()
		}
	}
}

// ConcatRecords stacks records with the same schema. Caller Releases the result.
func ConcatRecords(recs []arrow.Record) (arrow.Record, error) {
	if len(recs) == 0 {
		return nil, fmt.Errorf("no records to concat")
	}
	if len(recs) == 1 {
		recs[0].Retain()
		return recs[0], nil
	}
	var n int64
	schema := recs[0].Schema()
	for _, r := range recs {
		if !r.Schema().Equal(schema) {
			return nil, fmt.Errorf("concat schema mismatch")
		}
		n += r.NumRows()
	}
	idxAll := make([][]int, len(recs))
	// Build by appending each source row in order via builders.
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	for _, r := range recs {
		for row := 0; row < int(r.NumRows()); row++ {
			for c := 0; c < int(r.NumCols()); c++ {
				if err := appendValue(b.Field(c), r.Column(c), row); err != nil {
					return nil, err
				}
			}
		}
		_ = idxAll
		_ = n
	}
	return b.NewRecord(), nil
}

func appendValue(b array.Builder, arr arrow.Array, row int) error {
	c, err := cellFromArray(arr, row)
	if err != nil {
		return err
	}
	appendCellToBuilder(b, c, arr.DataType())
	return nil
}

// TopN keeps the best k rows under ORDER BY while streaming batches.
type TopN struct {
	k      int
	keys   []OrderKey
	items  topHeap
	schema *arrow.Schema
	ready  bool
}

type topItem struct {
	ord  []cell
	cols []cell
}

type topHeap struct {
	keys  []OrderKey
	items []topItem
}

func (h *topHeap) Len() int      { return len(h.items) }
func (h *topHeap) Swap(i, j int) { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *topHeap) Less(i, j int) bool {
	// max-heap: worst (largest for ASC) on top so we can evict it
	return cmpOrder(h.items[i].ord, h.items[j].ord, h.keys) > 0
}
func (h *topHeap) Push(x any) { h.items = append(h.items, x.(topItem)) }
func (h *topHeap) Pop() any {
	n := len(h.items)
	it := h.items[n-1]
	h.items = h.items[:n-1]
	return it
}

func cmpOrder(a, b []cell, keys []OrderKey) int {
	for i, k := range keys {
		cmp := cmpCell(a[i], b[i])
		if cmp == 0 {
			continue
		}
		if k.Desc {
			return -cmp
		}
		return cmp
	}
	return 0
}

// NewTopN prepares a heap that retains k rows.
func NewTopN(k int, keys []OrderKey) (*TopN, error) {
	if k < 0 {
		k = 0
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("top-n requires ORDER BY keys")
	}
	t := &TopN{k: k, keys: append([]OrderKey(nil), keys...)}
	t.items.keys = t.keys
	heap.Init(&t.items)
	return t, nil
}

// Add considers every row in rec.
func (t *TopN) Add(rec arrow.Record) error {
	if t.k == 0 || rec.NumRows() == 0 {
		if t.schema == nil && rec.Schema() != nil {
			t.schema = rec.Schema()
		}
		return nil
	}
	if t.schema == nil {
		t.schema = rec.Schema()
	}
	ord, cloned, err := orderArrays(rec, t.keys)
	if err != nil {
		return err
	}
	defer releaseAll(cloned)
	ncols := int(rec.NumCols())
	for row := 0; row < int(rec.NumRows()); row++ {
		okeys := make([]cell, len(t.keys))
		for i := range t.keys {
			c, err := cellFromArray(ord[i], row)
			if err != nil {
				return err
			}
			okeys[i] = c
		}
		if t.items.Len() == t.k && cmpOrder(okeys, t.items.items[0].ord, t.keys) >= 0 {
			continue
		}
		cols := make([]cell, ncols)
		for c := 0; c < ncols; c++ {
			cell, err := cellFromArray(rec.Column(c), row)
			if err != nil {
				return err
			}
			cols[c] = cell
		}
		it := topItem{ord: okeys, cols: cols}
		if t.items.Len() < t.k {
			heap.Push(&t.items, it)
			continue
		}
		heap.Pop(&t.items)
		heap.Push(&t.items, it)
	}
	t.ready = true
	return nil
}

// Finish materializes the heap in ORDER BY sequence. Caller Releases the record.
func (t *TopN) Finish() (arrow.Record, error) {
	if t.schema == nil {
		return nil, fmt.Errorf("top-n: no schema")
	}
	items := append([]topItem(nil), t.items.items...)
	sort.SliceStable(items, func(i, j int) bool {
		return cmpOrder(items[i].ord, items[j].ord, t.keys) < 0
	})
	b := array.NewRecordBuilder(memory.DefaultAllocator, t.schema)
	defer b.Release()
	for _, it := range items {
		for c, cell := range it.cols {
			appendCellToBuilder(b.Field(c), cell, t.schema.Field(c).Type)
		}
	}
	return b.NewRecord(), nil
}
