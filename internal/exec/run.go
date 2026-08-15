// Package exec runs a fixed Scan → Filter → Aggregate → Sort → Limit pipeline.
package exec

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/apache/arrow-go/v18/arrow"
)

// Request is a physical-ish plan for Phase 4 (no optimizer yet).
type Request struct {
	Table     *catalog.Table
	Where     expr.Expr
	ScanCols  []string // empty = all
	GroupBy   []string
	Aggs      []kernel.AggSpec
	Project   []string // output names after agg/scan; empty = keep
	Order     []kernel.OrderKey
	Limit     int64 // 0 = no SQL limit
	BatchSize int64
}

// Result is one output record plus scan stats.
type Result struct {
	Record arrow.Record
	Stats  parquetscan.Stats
	Groups int
}

// Run executes the pipeline. Caller must Release Result.Record.
func Run(ctx context.Context, req Request) (*Result, error) {
	if req.Table == nil {
		return nil, fmt.Errorf("exec: nil table")
	}
	if len(req.Table.Files) == 0 {
		return nil, fmt.Errorf("table %q has no parquet files", req.Table.Name)
	}
	isAgg := len(req.Aggs) > 0 || len(req.GroupBy) > 0
	scanCols := union(req.ScanCols, expr.Columns(req.Where))
	scanCols = union(scanCols, req.GroupBy)
	for _, a := range req.Aggs {
		if a.Input != "" {
			scanCols = union(scanCols, []string{a.Input})
		}
	}
	if !isAgg {
		scanCols = union(scanCols, req.Project)
		for _, k := range req.Order {
			scanCols = union(scanCols, []string{k.Name})
		}
	}
	if isAgg && len(scanCols) == 0 && len(req.Table.Fields) > 0 {
		// COUNT(*) still needs to iterate rows; read one column instead of *.
		scanCols = []string{req.Table.Fields[0].Name}
	}

	rdr, err := parquetscan.Open(ctx, req.Table.Files, parquetscan.Options{
		Columns:   scanCols,
		BatchSize: req.BatchSize,
	})
	if err != nil {
		return nil, err
	}
	defer rdr.Close()

	var agg *kernel.HashAgg
	if isAgg {
		agg, err = kernel.NewHashAgg(req.GroupBy, req.Aggs)
		if err != nil {
			return nil, err
		}
	}
	var top *kernel.TopN
	useTop := !isAgg && len(req.Order) > 0 && req.Limit > 0
	if useTop {
		top, err = kernel.NewTopN(int(req.Limit), req.Order)
		if err != nil {
			return nil, err
		}
	}
	var collected []arrow.Record
	var kept int64
	stopEarly := !isAgg && !useTop && req.Limit > 0 && len(req.Order) == 0

	for {
		if stopEarly && kept >= req.Limit {
			break
		}
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			releaseAll(collected)
			return nil, err
		}
		cur := rec
		if req.Where != nil {
			mask, err := kernel.Eval(req.Where, rec)
			if err != nil {
				rec.Release()
				releaseAll(collected)
				return nil, err
			}
			compacted, err := kernel.Compact(rec, mask)
			mask.Release()
			rec.Release()
			if err != nil {
				releaseAll(collected)
				return nil, err
			}
			cur = compacted
		}
		if cur.NumRows() == 0 {
			cur.Release()
			continue
		}
		if isAgg {
			err := agg.Add(cur)
			cur.Release()
			if err != nil {
				return nil, err
			}
			continue
		}
		if useTop {
			err := top.Add(cur)
			cur.Release()
			if err != nil {
				return nil, err
			}
			continue
		}
		if stopEarly {
			need := req.Limit - kept
			if cur.NumRows() > need {
				trimmed, err := kernel.LimitRecord(cur, need)
				cur.Release()
				if err != nil {
					releaseAll(collected)
					return nil, err
				}
				cur = trimmed
			}
		}
		kept += cur.NumRows()
		collected = append(collected, cur)
	}

	var out arrow.Record
	groups := 0
	switch {
	case isAgg:
		out, err = agg.Finish()
		if err != nil {
			return nil, err
		}
		groups = agg.NumGroups()
		if len(req.Order) > 0 {
			sorted, err := kernel.SortRecord(out, req.Order)
			out.Release()
			if err != nil {
				return nil, err
			}
			out = sorted
		}
		if req.Limit > 0 {
			lim, err := kernel.LimitRecord(out, req.Limit)
			out.Release()
			if err != nil {
				return nil, err
			}
			out = lim
		}
	case useTop:
		out, err = top.Finish()
		if err != nil {
			return nil, err
		}
	default:
		if len(collected) == 0 {
			// empty result: still need a schema — scan one empty projection via reader schema
			schema := rdr.Schema()
			if schema == nil {
				return nil, fmt.Errorf("empty scan with no schema")
			}
			empty := emptyRecord(schema)
			out = empty
		} else if len(req.Order) > 0 {
			cat, err := kernel.ConcatRecords(collected)
			releaseAll(collected)
			collected = nil
			if err != nil {
				return nil, err
			}
			sorted, err := kernel.SortRecord(cat, req.Order)
			cat.Release()
			if err != nil {
				return nil, err
			}
			out = sorted
			if req.Limit > 0 {
				lim, err := kernel.LimitRecord(out, req.Limit)
				out.Release()
				if err != nil {
					return nil, err
				}
				out = lim
			}
		} else {
			out, err = kernel.ConcatRecords(collected)
			releaseAll(collected)
			collected = nil
			if err != nil {
				return nil, err
			}
		}
	}

	if len(req.Project) > 0 {
		proj, err := kernel.Project(out, req.Project)
		out.Release()
		if err != nil {
			return nil, err
		}
		out = proj
	}

	return &Result{Record: out, Stats: rdr.Stats(), Groups: groups}, nil
}

func emptyRecord(schema *arrow.Schema) arrow.Record {
	// LimitRecord-style empty: concat of nothing — build via Limit of a zero-col trick
	// Use Concat on no rows: Project from a 0-row record isn't available; construct via Sort on empty isn't either.
	// parquet reader schema with 0 rows: we synthesize using LimitRecord after...
	// Simplest: NewRecord with empty arrays via RecordBuilder happens in HashAgg; for scan,
	// create via ConcatRecords is wrong. Use kernel helper.
	return kernel.EmptyRecord(schema)
}

func union(parts ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range parts {
		for _, s := range p {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func releaseAll(recs []arrow.Record) {
	for _, r := range recs {
		if r != nil {
			r.Release()
		}
	}
}
