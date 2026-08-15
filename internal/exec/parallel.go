package exec

import (
	"context"
	"io"
	"sync"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

type morsel struct {
	file string
	rg   int
}

type workOut struct {
	agg   *kernel.HashAgg
	recs  []arrow.Record
	stats parquetscan.Stats
	err   error
}

func listMorsels(req Request) []morsel {
	if req.Empty || req.Table == nil {
		return nil
	}
	var out []morsel
	if req.RowGroups != nil {
		for _, f := range req.Table.Files {
			for _, i := range req.RowGroups[f] {
				out = append(out, morsel{file: f, rg: i})
			}
		}
		return out
	}
	if len(req.Table.RowGroups) > 0 {
		for _, rg := range req.Table.RowGroups {
			out = append(out, morsel{file: rg.File, rg: rg.Index})
		}
		return out
	}
	// Fallback: one morsel per file (read all groups in that file).
	for _, f := range req.Table.Files {
		out = append(out, morsel{file: f, rg: -1})
	}
	return out
}

func runParallel(ctx context.Context, req Request, prep prepared, jobs int) (*Result, error) {
	return runWorkers(ctx, req, prep, jobs, false)
}

func runRowParallel(ctx context.Context, req Request, prep prepared, jobs int) (*Result, error) {
	return runWorkers(ctx, req, prep, jobs, true)
}

func runWorkers(ctx context.Context, req Request, prep prepared, jobs int, rowEngine bool) (*Result, error) {
	morsels := listMorsels(req)
	if len(morsels) == 0 {
		if rowEngine {
			return runRowSerial(ctx, req, prep)
		}
		return runVecSerial(ctx, req, prep)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := make(chan morsel)
	outCh := make(chan workOut, jobs)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range ch {
				wo := processMorsel(ctx, req, prep, m, rowEngine)
				if wo.err != nil {
					cancel()
				}
				outCh <- wo
			}
		}()
	}
	go func() {
		defer func() {
			close(ch)
			wg.Wait()
			close(outCh)
		}()
		for _, m := range morsels {
			select {
			case <-ctx.Done():
				return
			case ch <- m:
			}
		}
	}()

	merged, err := newSink(req, prep)
	if err != nil {
		return nil, err
	}
	var st parquetscan.Stats
	var schema *arrow.Schema
	for wo := range outCh {
		if wo.err != nil && err == nil {
			err = wo.err
		}
		if wo.err != nil {
			releaseAll(wo.recs)
			continue
		}
		st = mergeStats(st, wo.stats)
		if wo.stats.ColumnNames != nil && st.ColumnNames == nil {
			st.ColumnNames = wo.stats.ColumnNames
			st.ColumnsRead = wo.stats.ColumnsRead
		}
		if prep.isAgg {
			if merged.agg != nil && wo.agg != nil {
				if merr := merged.agg.Merge(wo.agg); merr != nil && err == nil {
					err = merr
				}
			}
			continue
		}
		merged.collected = append(merged.collected, wo.recs...)
		for _, r := range wo.recs {
			merged.kept += r.NumRows()
			if schema == nil {
				schema = r.Schema()
			}
		}
	}
	if err != nil {
		merged.release()
		return nil, err
	}
	if schema == nil && req.Table != nil {
		// all morsels empty: open schema only
		rdr, oerr := parquetscan.Open(ctx, req.Table.Files, scanOptions(emptyReq(req), prep))
		if oerr != nil {
			merged.release()
			return nil, oerr
		}
		schema = rdr.Schema()
		rdr.Close()
	}
	out, groups, err := merged.finish(schema)
	if err != nil {
		return nil, err
	}
	return &Result{Record: out, Stats: overlayCatalogStats(st, req), Groups: groups}, nil
}

func emptyReq(req Request) Request {
	e := req
	e.Empty = true
	return e
}

func processMorsel(ctx context.Context, req Request, prep prepared, m morsel, rowEngine bool) workOut {
	opts := parquetscan.Options{
		Columns:   prep.scanCols,
		BatchSize: req.BatchSize,
	}
	if m.rg >= 0 {
		opts.RowGroups = map[string][]int{m.file: {m.rg}}
	}
	rdr, err := parquetscan.Open(ctx, []string{m.file}, opts)
	if err != nil {
		return workOut{err: err}
	}
	defer rdr.Close()

	local := req
	local.Limit = 0 // apply limit after merge
	local.Order = nil
	snk, err := newSink(local, prep)
	if err != nil {
		return workOut{err: err}
	}
	for {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			snk.release()
			return workOut{err: err}
		}
		if rowEngine {
			err = snk.addRow(rec)
		} else {
			err = snk.addVec(rec)
		}
		if err != nil {
			snk.release()
			return workOut{err: err}
		}
	}
	st := rdr.Stats()
	if prep.isAgg {
		return workOut{agg: snk.agg, stats: st}
	}
	return workOut{recs: snk.collected, stats: st}
}

func mergeStats(a, b parquetscan.Stats) parquetscan.Stats {
	a.FilesOpened += b.FilesOpened
	a.RowGroupsRead += b.RowGroupsRead
	a.RowsRead += b.RowsRead
	a.BatchesEmitted += b.BatchesEmitted
	a.CompressedBytes += b.CompressedBytes
	if a.ColumnsRead == 0 {
		a.ColumnsRead = b.ColumnsRead
		a.ColumnNames = b.ColumnNames
	}
	return a
}

func boolMask(sel []bool) *array.Boolean {
	b := array.NewBooleanBuilder(memory.DefaultAllocator)
	defer b.Release()
	b.Reserve(len(sel))
	for _, v := range sel {
		b.Append(v)
	}
	return b.NewBooleanArray()
}
