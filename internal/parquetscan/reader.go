package parquetscan

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

const DefaultBatchSize int64 = 8192

// Options control a scan.
type Options struct {
	// Columns are field names to read. Empty means all columns.
	Columns []string
	// BatchSize is rows per Arrow record. Zero uses DefaultBatchSize.
	BatchSize int64
	// Alloc is used for Arrow buffers. Nil uses memory.DefaultAllocator.
	Alloc memory.Allocator
	// RowGroups maps file path → row-group indices to read.
	// A nil map means read every row group. An empty slice for a file skips it.
	RowGroups map[string][]int
}

func (o Options) batchSize() int64 {
	if o.BatchSize > 0 {
		return o.BatchSize
	}
	return DefaultBatchSize
}

func (o Options) alloc() memory.Allocator {
	if o.Alloc != nil {
		return o.Alloc
	}
	return memory.DefaultAllocator
}

func (o Options) keptGroups(path string) []int {
	if o.RowGroups == nil {
		return nil // all
	}
	kept, ok := o.RowGroups[path]
	if !ok {
		return nil
	}
	out := append([]int(nil), kept...)
	if out == nil {
		out = []int{}
	}
	return out
}

// Stats are populated as a scan runs.
type Stats struct {
	FilesOpened      int
	FilesTotal       int
	RowGroupsRead    int
	RowGroupsTotal   int
	RowGroupsSkipped int
	RowsRead         int64
	BatchesEmitted   int
	ColumnsRead      int
	CompressedBytes  int64 // on-disk size of selected column chunks that were read
	ColumnNames      []string
}

// Reader streams Arrow records from one or more Parquet files.
//
// Next returns a retained record; the caller must Release it.
// Next returns (nil, io.EOF) when exhausted.
type Reader struct {
	files   []string
	opts    Options
	indices []int // nil = all columns

	fileIdx int
	cur     pqarrow.RecordReader
	closers []io.Closer
	schema  *arrow.Schema
	stats   Stats
	err     error
	eof     bool
}

// Open starts a scan over parquet files in order.
func Open(ctx context.Context, files []string, opts Options) (*Reader, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no parquet files")
	}
	r := &Reader{files: files, opts: opts}
	r.stats.FilesTotal = len(files)
	if err := r.openFile(ctx, 0); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

func (r *Reader) openFile(ctx context.Context, i int) error {
	for {
		r.closeCurrent()
		if i >= len(r.files) {
			r.eof = true
			return nil
		}
		path := r.files[i]
		pf, err := file.OpenParquetFile(path, false)
		if err != nil {
			return fmt.Errorf("open parquet %s: %w", path, err)
		}
		nrg := pf.NumRowGroups()
		r.stats.RowGroupsTotal += nrg
		kept := r.opts.keptGroups(path)
		if kept != nil && len(kept) == 0 {
			if r.schema == nil {
				props := pqarrow.ArrowReadProperties{BatchSize: r.opts.batchSize()}
				ar, err := pqarrow.NewFileReader(pf, props, r.opts.alloc())
				if err != nil {
					pf.Close()
					return fmt.Errorf("arrow reader %s: %w", path, err)
				}
				schema, err := ar.Schema()
				if err != nil {
					pf.Close()
					return err
				}
				r.schema = schema
				idx, err := columnIndices(schema, r.opts.Columns)
				if err != nil {
					pf.Close()
					return err
				}
				r.indices = idx
				r.stats.ColumnsRead = schema.NumFields()
				if idx != nil {
					r.stats.ColumnsRead = len(idx)
				}
				r.stats.ColumnNames = selectedNames(schema, idx)
			}
			r.stats.RowGroupsSkipped += nrg
			pf.Close()
			i++
			continue
		}

		props := pqarrow.ArrowReadProperties{BatchSize: r.opts.batchSize()}
		ar, err := pqarrow.NewFileReader(pf, props, r.opts.alloc())
		if err != nil {
			pf.Close()
			return fmt.Errorf("arrow reader %s: %w", path, err)
		}
		schema, err := ar.Schema()
		if err != nil {
			pf.Close()
			return err
		}
		if r.schema == nil {
			r.schema = schema
			idx, err := columnIndices(schema, r.opts.Columns)
			if err != nil {
				pf.Close()
				return err
			}
			r.indices = idx
			r.stats.ColumnsRead = schema.NumFields()
			if idx != nil {
				r.stats.ColumnsRead = len(idx)
			}
			r.stats.ColumnNames = selectedNames(schema, idx)
		}

		rr, err := ar.GetRecordReader(ctx, r.indices, kept)
		if err != nil {
			pf.Close()
			return fmt.Errorf("record reader %s: %w", path, err)
		}
		r.closers = append(r.closers, pf)
		r.cur = rr
		r.fileIdx = i
		r.stats.FilesOpened++
		if kept == nil {
			r.stats.RowGroupsRead += nrg
			r.stats.CompressedBytes += selectedCompressedBytes(pf, r.indices, nil)
		} else {
			r.stats.RowGroupsRead += len(kept)
			r.stats.RowGroupsSkipped += nrg - len(kept)
			r.stats.CompressedBytes += selectedCompressedBytes(pf, r.indices, kept)
		}
		return nil
	}
}

func selectedNames(schema *arrow.Schema, idx []int) []string {
	if idx == nil {
		names := make([]string, schema.NumFields())
		for i := 0; i < schema.NumFields(); i++ {
			names[i] = schema.Field(i).Name
		}
		return names
	}
	names := make([]string, len(idx))
	for i, c := range idx {
		names[i] = schema.Field(c).Name
	}
	return names
}

func selectedCompressedBytes(pf *file.Reader, idx []int, rowGroups []int) int64 {
	meta := pf.MetaData()
	var total int64
	wantCol := map[int]struct{}{}
	if idx != nil {
		for _, c := range idx {
			wantCol[c] = struct{}{}
		}
	}
	wantRG := map[int]struct{}{}
	if rowGroups != nil {
		for _, g := range rowGroups {
			wantRG[g] = struct{}{}
		}
	}
	for i := 0; i < pf.NumRowGroups(); i++ {
		if rowGroups != nil {
			if _, ok := wantRG[i]; !ok {
				continue
			}
		}
		rg := meta.RowGroup(i)
		for c := 0; c < rg.NumColumns(); c++ {
			if idx != nil {
				if _, ok := wantCol[c]; !ok {
					continue
				}
			}
			chunk, err := rg.ColumnChunk(c)
			if err != nil {
				continue
			}
			total += chunk.TotalCompressedSize()
		}
	}
	return total
}

func columnIndices(schema *arrow.Schema, names []string) ([]int, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]int, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		idxs := schema.FieldIndices(name)
		if len(idxs) == 0 {
			have := make([]string, schema.NumFields())
			for i := 0; i < schema.NumFields(); i++ {
				have[i] = schema.Field(i).Name
			}
			return nil, fmt.Errorf("unknown column %q (have %s)", name, strings.Join(have, ", "))
		}
		out = append(out, idxs[0])
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (r *Reader) Schema() *arrow.Schema { return r.schema }

func (r *Reader) Stats() Stats { return r.stats }

// Next returns the next Arrow record. Caller must Release the record.
func (r *Reader) Next() (arrow.Record, error) {
	if r.err != nil {
		return nil, r.err
	}
	for {
		if r.eof || r.cur == nil {
			return nil, io.EOF
		}
		if r.cur.Next() {
			rec := r.cur.Record()
			if rec == nil {
				continue
			}
			rec.Retain()
			r.stats.RowsRead += rec.NumRows()
			r.stats.BatchesEmitted++
			return rec, nil
		}
		if err := r.cur.Err(); err != nil && err != io.EOF {
			r.err = err
			return nil, err
		}
		if err := r.openFile(context.Background(), r.fileIdx+1); err != nil {
			r.err = err
			return nil, err
		}
	}
}

func (r *Reader) closeCurrent() {
	if r.cur != nil {
		r.cur.Release()
		r.cur = nil
	}
}

// Close releases file handles and the current record reader.
func (r *Reader) Close() error {
	r.closeCurrent()
	var first error
	for i := len(r.closers) - 1; i >= 0; i-- {
		if err := r.closers[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	r.closers = nil
	return first
}
