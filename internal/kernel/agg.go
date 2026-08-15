package kernel

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// AggFn is a supported aggregate.
type AggFn uint8

const (
	AggCountStar AggFn = iota
	AggCount
	AggSum
	AggAvg
	AggMin
	AggMax
)

func (f AggFn) String() string {
	switch f {
	case AggCountStar:
		return "count_star"
	case AggCount:
		return "count"
	case AggSum:
		return "sum"
	case AggAvg:
		return "avg"
	case AggMin:
		return "min"
	case AggMax:
		return "max"
	default:
		return "?"
	}
}

// AggSpec is one output aggregate column.
type AggSpec struct {
	Fn    AggFn
	Input string // ignored for COUNT(*)
	Name  string // output field name
}

// ParseAggList parses CLI --agg values: count, count(*), sum(amount_cents), avg(x).
func ParseAggList(s string) ([]AggSpec, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty --agg list")
	}
	var out []AggSpec
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		spec, err := parseOneAgg(part)
		if err != nil {
			return nil, err
		}
		out = append(out, spec)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty --agg list")
	}
	return out, nil
}

func parseOneAgg(part string) (AggSpec, error) {
	low := strings.ToLower(part)
	if low == "count" || low == "count(*)" || low == "count_star" {
		return AggSpec{Fn: AggCountStar, Name: "count"}, nil
	}
	lparen := strings.IndexByte(part, '(')
	rparen := strings.LastIndexByte(part, ')')
	if lparen < 0 || rparen != len(part)-1 || rparen <= lparen+1 {
		return AggSpec{}, fmt.Errorf("invalid agg %q (want count or sum(col))", part)
	}
	fn := strings.ToLower(strings.TrimSpace(part[:lparen]))
	arg := strings.TrimSpace(part[lparen+1 : rparen])
	spec := AggSpec{Input: arg}
	switch fn {
	case "count":
		if arg == "*" {
			spec.Fn = AggCountStar
			spec.Name = "count"
			spec.Input = ""
		} else {
			spec.Fn = AggCount
			spec.Name = "count_" + arg
		}
	case "sum":
		spec.Fn = AggSum
		spec.Name = "sum_" + arg
	case "avg":
		spec.Fn = AggAvg
		spec.Name = "avg_" + arg
	case "min":
		spec.Fn = AggMin
		spec.Name = "min_" + arg
	case "max":
		spec.Fn = AggMax
		spec.Name = "max_" + arg
	default:
		return AggSpec{}, fmt.Errorf("unknown aggregate %q", fn)
	}
	if spec.Fn != AggCountStar && arg == "*" {
		return AggSpec{}, fmt.Errorf("%s(*) is not supported", fn)
	}
	return spec, nil
}

type accKind uint8

const (
	accNone accKind = iota
	accI64
	accF64
	accStr
	accBool
)

type acc struct {
	fn         AggFn
	kind       accKind
	n          int64
	sumI       int64
	sumF       float64
	minI, maxI int64
	minF, maxF float64
	minS, maxS string
	minB, maxB bool
	seen       bool
}

func (a *acc) add(arr arrow.Array, row int) error {
	if a.fn == AggCountStar {
		a.n++
		return nil
	}
	if arr == nil {
		return fmt.Errorf("aggregate missing input array")
	}
	if arr.IsNull(row) {
		return nil
	}
	c, err := cellFromArray(arr, row)
	if err != nil {
		return err
	}
	if c.kind == cellNull {
		return nil
	}
	if !a.seen {
		switch c.kind {
		case cellI64:
			a.kind = accI64
		case cellF64:
			a.kind = accF64
		case cellStr:
			a.kind = accStr
		case cellBool:
			a.kind = accBool
		}
	}
	a.n++
	switch a.fn {
	case AggCount:
		a.seen = true
		return nil
	case AggSum, AggAvg:
		switch c.kind {
		case cellI64:
			a.sumI += c.i64
			a.sumF += float64(c.i64)
			a.kind = accI64
		case cellF64:
			a.sumF += c.f64
			a.kind = accF64
		default:
			return fmt.Errorf("SUM/AVG requires a numeric column")
		}
		a.seen = true
	case AggMin:
		if !a.seen {
			a.setMinMax(c)
			a.seen = true
			break
		}
		if cmpCell(c, a.minCell()) < 0 {
			a.setMinMax(c)
		}
	case AggMax:
		if !a.seen {
			a.setMinMax(c)
			a.seen = true
			break
		}
		if cmpCell(c, a.maxCell()) > 0 {
			a.setMinMax(c)
		}
	}
	return nil
}

func (a *acc) setMinMax(c cell) {
	switch c.kind {
	case cellI64:
		a.kind = accI64
		a.minI, a.maxI = c.i64, c.i64
	case cellF64:
		a.kind = accF64
		a.minF, a.maxF = c.f64, c.f64
	case cellStr:
		a.kind = accStr
		a.minS, a.maxS = c.str, c.str
	case cellBool:
		a.kind = accBool
		a.minB, a.maxB = c.b, c.b
	}
}

func (a *acc) minCell() cell {
	switch a.kind {
	case accI64:
		return cell{kind: cellI64, i64: a.minI}
	case accF64:
		return cell{kind: cellF64, f64: a.minF}
	case accStr:
		return cell{kind: cellStr, str: a.minS}
	case accBool:
		return cell{kind: cellBool, b: a.minB}
	default:
		return cell{kind: cellNull}
	}
}

func (a *acc) maxCell() cell {
	switch a.kind {
	case accI64:
		return cell{kind: cellI64, i64: a.maxI}
	case accF64:
		return cell{kind: cellF64, f64: a.maxF}
	case accStr:
		return cell{kind: cellStr, str: a.maxS}
	case accBool:
		return cell{kind: cellBool, b: a.maxB}
	default:
		return cell{kind: cellNull}
	}
}

type groupRow struct {
	keys []cell
	aggs []acc
}

// HashAgg accumulates GROUP BY keys and aggregates across Arrow batches.
type HashAgg struct {
	Keys     []string
	Aggs     []AggSpec
	keyTypes []arrow.DataType
	idx      map[string]int
	rows     []groupRow
	buf      []byte
}

// NewHashAgg starts an empty aggregator.
func NewHashAgg(keys []string, aggs []AggSpec) (*HashAgg, error) {
	if len(aggs) == 0 && len(keys) == 0 {
		return nil, fmt.Errorf("aggregate requires keys or agg functions")
	}
	names := map[string]struct{}{}
	for _, k := range keys {
		if k == "" {
			return nil, fmt.Errorf("empty group key")
		}
		if _, ok := names[k]; ok {
			return nil, fmt.Errorf("duplicate group key %q", k)
		}
		names[k] = struct{}{}
	}
	for i := range aggs {
		if aggs[i].Name == "" {
			return nil, fmt.Errorf("aggregate %d missing output name", i)
		}
		if _, ok := names[aggs[i].Name]; ok {
			return nil, fmt.Errorf("duplicate output column %q", aggs[i].Name)
		}
		names[aggs[i].Name] = struct{}{}
		if aggs[i].Fn != AggCountStar && aggs[i].Input == "" {
			return nil, fmt.Errorf("aggregate %s missing input column", aggs[i].Fn)
		}
	}
	return &HashAgg{
		Keys: append([]string(nil), keys...),
		Aggs: append([]AggSpec(nil), aggs...),
		idx:  map[string]int{},
	}, nil
}

// Add consumes one batch.
func (h *HashAgg) Add(rec arrow.Record) error {
	if rec.NumRows() == 0 {
		if h.keyTypes == nil && rec.Schema() != nil {
			if err := h.captureKeyTypes(rec); err != nil {
				return err
			}
		}
		return nil
	}
	if err := h.captureKeyTypes(rec); err != nil {
		return err
	}
	keyArrs := make([]arrow.Array, len(h.Keys))
	cloned := make([]arrow.Array, 0, len(h.Keys))
	for i, name := range h.Keys {
		arr, err := colByName(rec, name)
		if err != nil {
			return err
		}
		var extra bool
		arr, extra = unwrapDict(arr)
		if extra {
			cloned = append(cloned, arr)
		}
		keyArrs[i] = arr
	}
	defer func() {
		for _, a := range cloned {
			a.Release()
		}
	}()
	aggArrs := make([]arrow.Array, len(h.Aggs))
	aggCloned := make([]arrow.Array, 0, len(h.Aggs))
	for i, spec := range h.Aggs {
		if spec.Fn == AggCountStar {
			continue
		}
		arr, err := colByName(rec, spec.Input)
		if err != nil {
			return err
		}
		var extra bool
		arr, extra = unwrapDict(arr)
		if extra {
			aggCloned = append(aggCloned, arr)
		}
		aggArrs[i] = arr
	}
	defer func() {
		for _, a := range aggCloned {
			a.Release()
		}
	}()

	n := int(rec.NumRows())
	for row := 0; row < n; row++ {
		h.buf = h.buf[:0]
		keys := make([]cell, len(h.Keys))
		for i, arr := range keyArrs {
			c, err := cellFromArray(arr, row)
			if err != nil {
				return err
			}
			keys[i] = c
			h.buf = appendCellKey(h.buf, c)
		}
		k := string(h.buf)
		gi, ok := h.idx[k]
		if !ok {
			g := groupRow{keys: keys, aggs: make([]acc, len(h.Aggs))}
			for i, spec := range h.Aggs {
				g.aggs[i].fn = spec.Fn
			}
			gi = len(h.rows)
			h.idx[k] = gi
			h.rows = append(h.rows, g)
		}
		for i := range h.Aggs {
			if err := h.rows[gi].aggs[i].add(aggArrs[i], row); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *HashAgg) captureKeyTypes(rec arrow.Record) error {
	if h.keyTypes != nil {
		return nil
	}
	h.keyTypes = make([]arrow.DataType, len(h.Keys))
	for i, name := range h.Keys {
		arr, err := colByName(rec, name)
		if err != nil {
			return err
		}
		dt := arr.DataType()
		if d, ok := arr.(*array.Dictionary); ok {
			dt = d.Dictionary().DataType()
		}
		h.keyTypes[i] = dt
	}
	return nil
}

func (h *HashAgg) ensureScalarGroup() {
	if len(h.Keys) != 0 || len(h.rows) != 0 {
		return
	}
	g := groupRow{aggs: make([]acc, len(h.Aggs))}
	for i, spec := range h.Aggs {
		g.aggs[i].fn = spec.Fn
	}
	h.rows = append(h.rows, g)
	h.idx[""] = 0
}

// Finish builds one output record. Caller must Release it.
func (h *HashAgg) Finish() (arrow.Record, error) {
	if len(h.Keys) == 0 {
		h.ensureScalarGroup()
	}
	fields := make([]arrow.Field, 0, len(h.Keys)+len(h.Aggs))
	for i, name := range h.Keys {
		dt := h.keyTypes[i]
		if dt == nil {
			dt = arrow.BinaryTypes.String
		}
		fields = append(fields, arrow.Field{Name: name, Type: dt, Nullable: true})
	}
	for i, spec := range h.Aggs {
		fields = append(fields, arrow.Field{Name: spec.Name, Type: h.aggOutType(i), Nullable: spec.Fn != AggCountStar && spec.Fn != AggCount})
	}
	schema := arrow.NewSchema(fields, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	for _, g := range h.rows {
		for i, c := range g.keys {
			appendCellToBuilder(b.Field(i), c, h.keyTypes[i])
		}
		for i, a := range g.aggs {
			appendAcc(b.Field(len(h.Keys)+i), h.Aggs[i], a)
		}
	}
	return b.NewRecord(), nil
}

func (h *HashAgg) aggOutType(i int) arrow.DataType {
	spec := h.Aggs[i]
	switch spec.Fn {
	case AggCountStar, AggCount:
		return arrow.PrimitiveTypes.Int64
	case AggAvg:
		return arrow.PrimitiveTypes.Float64
	case AggSum:
		if len(h.rows) > 0 {
			switch h.rows[0].aggs[i].kind {
			case accF64:
				return arrow.PrimitiveTypes.Float64
			}
		}
		return arrow.PrimitiveTypes.Int64
	case AggMin, AggMax:
		if len(h.rows) > 0 {
			switch h.rows[0].aggs[i].kind {
			case accF64:
				return arrow.PrimitiveTypes.Float64
			case accStr:
				return arrow.BinaryTypes.String
			case accBool:
				return arrow.FixedWidthTypes.Boolean
			}
		}
		if i < len(h.keyTypes) {
			// fallback
		}
		return arrow.PrimitiveTypes.Int64
	default:
		return arrow.PrimitiveTypes.Int64
	}
}

func appendCellToBuilder(b array.Builder, c cell, dt arrow.DataType) {
	if c.kind == cellNull {
		b.AppendNull()
		return
	}
	switch x := b.(type) {
	case *array.Int64Builder:
		x.Append(c.i64)
	case *array.TimestampBuilder:
		x.Append(arrow.Timestamp(c.i64))
	case *array.Float64Builder:
		if c.kind == cellF64 {
			x.Append(c.f64)
		} else {
			x.Append(float64(c.i64))
		}
	case *array.StringBuilder:
		x.Append(c.str)
	case *array.BooleanBuilder:
		x.Append(c.b)
	default:
		b.AppendNull()
	}
}

func appendAcc(b array.Builder, spec AggSpec, a acc) {
	switch spec.Fn {
	case AggCountStar, AggCount:
		b.(*array.Int64Builder).Append(a.n)
	case AggSum:
		if a.n == 0 {
			b.AppendNull()
			return
		}
		switch x := b.(type) {
		case *array.Float64Builder:
			x.Append(a.sumF)
		case *array.Int64Builder:
			x.Append(a.sumI)
		default:
			b.AppendNull()
		}
	case AggAvg:
		if a.n == 0 {
			b.AppendNull()
			return
		}
		b.(*array.Float64Builder).Append(a.sumF / float64(a.n))
	case AggMin:
		if !a.seen {
			b.AppendNull()
			return
		}
		appendCellToBuilder(b, a.minCell(), nil)
	case AggMax:
		if !a.seen {
			b.AppendNull()
			return
		}
		appendCellToBuilder(b, a.maxCell(), nil)
	}
}

// NumGroups is the number of accumulated groups (0 before any rows unless scalar).
func (h *HashAgg) NumGroups() int { return len(h.rows) }
