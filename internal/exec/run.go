// Package exec runs Scan → Filter → Aggregate → Sort → Limit.
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

const (
	EngineVectorized = "vectorized"
	EngineRow        = "row"
)

// Request is a physical plan for the execution pipeline.
type Request struct {
	Table     *catalog.Table
	Where     expr.Expr
	ScanCols  []string // empty = all
	GroupBy   []string
	Aggs      []kernel.AggSpec
	Project   []string
	Order     []kernel.OrderKey
	Limit     int64
	BatchSize int64
	// RowGroups maps file path → row-group indices to read (nil = all).
	RowGroups map[string][]int
	// Empty is a constant-false plan: return a 0-row result without scanning pages.
	Empty bool
	// Engine is "vectorized" (default) or "row".
	Engine string
	// Jobs is the worker count. <=0 means sequential (1 worker).
	Jobs int
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
	prep, err := prepare(req)
	if err != nil {
		return nil, err
	}
	jobs := req.Jobs
	if jobs < 1 {
		jobs = 1
	}
	nMorsel := len(listMorsels(req))
	if req.Empty {
		nMorsel = 0
	}
	if jobs > 1 && nMorsel > 1 {
		if jobs > nMorsel {
			jobs = nMorsel
		}
		req.Jobs = jobs
		if req.Engine == EngineRow {
			return runRowParallel(ctx, req, prep, jobs)
		}
		return runParallel(ctx, req, prep, jobs)
	}
	if req.Engine == EngineRow {
		return runRowSerial(ctx, req, prep)
	}
	return runVecSerial(ctx, req, prep)
}

type prepared struct {
	scanCols []string
	isAgg    bool
}

func prepare(req Request) (prepared, error) {
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
		scanCols = []string{req.Table.Fields[0].Name}
	}
	return prepared{scanCols: scanCols, isAgg: isAgg}, nil
}

func scanOptions(req Request, prep prepared) parquetscan.Options {
	rowGroups := req.RowGroups
	if req.Empty {
		rowGroups = map[string][]int{}
		for _, f := range req.Table.Files {
			rowGroups[f] = []int{}
		}
	}
	return parquetscan.Options{
		Columns:   prep.scanCols,
		BatchSize: req.BatchSize,
		RowGroups: rowGroups,
	}
}

func overlayCatalogStats(st parquetscan.Stats, req Request) parquetscan.Stats {
	if req.Table != nil {
		st.FilesTotal = len(req.Table.Files)
		st.RowGroupsTotal = req.Table.NumRowGroups
		if st.RowGroupsRead <= st.RowGroupsTotal {
			st.RowGroupsSkipped = st.RowGroupsTotal - st.RowGroupsRead
		}
	}
	return st
}

func runVecSerial(ctx context.Context, req Request, prep prepared) (*Result, error) {
	rdr, err := parquetscan.Open(ctx, req.Table.Files, scanOptions(req, prep))
	if err != nil {
		return nil, err
	}
	defer rdr.Close()

	sink, err := newSink(req, prep)
	if err != nil {
		return nil, err
	}
	for {
		if sink.stopEarly && sink.kept >= req.Limit {
			break
		}
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			sink.release()
			return nil, err
		}
		if err := sink.addVec(rec); err != nil {
			rec.Release()
			sink.release()
			return nil, err
		}
	}
	out, groups, err := sink.finish(rdr.Schema())
	if err != nil {
		return nil, err
	}
	return &Result{Record: out, Stats: overlayCatalogStats(rdr.Stats(), req), Groups: groups}, nil
}

type sink struct {
	req       Request
	prep      prepared
	agg       *kernel.HashAgg
	top       *kernel.TopN
	useTop    bool
	stopEarly bool
	collected []arrow.Record
	kept      int64
}

func newSink(req Request, prep prepared) (*sink, error) {
	s := &sink{req: req, prep: prep}
	var err error
	if prep.isAgg {
		s.agg, err = kernel.NewHashAgg(req.GroupBy, req.Aggs)
		if err != nil {
			return nil, err
		}
	}
	s.useTop = !prep.isAgg && len(req.Order) > 0 && req.Limit > 0
	if s.useTop {
		s.top, err = kernel.NewTopN(int(req.Limit), req.Order)
		if err != nil {
			return nil, err
		}
	}
	s.stopEarly = !prep.isAgg && !s.useTop && req.Limit > 0 && len(req.Order) == 0
	return s, nil
}

func (s *sink) addVec(rec arrow.Record) error {
	cur := rec
	if s.req.Where != nil {
		mask, err := kernel.Eval(s.req.Where, rec)
		if err != nil {
			return err
		}
		compacted, err := kernel.Compact(rec, mask)
		mask.Release()
		rec.Release()
		if err != nil {
			return err
		}
		cur = compacted
	}
	return s.take(cur)
}

func (s *sink) addRow(rec arrow.Record) error {
	n := int(rec.NumRows())
	if s.req.Where == nil && s.prep.isAgg {
		for i := 0; i < n; i++ {
			if err := s.agg.AddRecordRow(rec, i); err != nil {
				rec.Release()
				return err
			}
		}
		rec.Release()
		return nil
	}
	if s.req.Where == nil {
		return s.take(rec)
	}
	// Row-at-a-time predicate, then gather survivors.
	sel := make([]bool, n)
	any := false
	for i := 0; i < n; i++ {
		keep, err := kernel.KeepRow(s.req.Where, rec, i)
		if err != nil {
			rec.Release()
			return err
		}
		sel[i] = keep
		any = any || keep
	}
	if s.prep.isAgg {
		for i := 0; i < n; i++ {
			if !sel[i] {
				continue
			}
			if err := s.agg.AddRecordRow(rec, i); err != nil {
				rec.Release()
				return err
			}
		}
		rec.Release()
		return nil
	}
	if !any {
		rec.Release()
		return nil
	}
	mask := boolMask(sel)
	compacted, err := kernel.Compact(rec, mask)
	mask.Release()
	rec.Release()
	if err != nil {
		return err
	}
	return s.take(compacted)
}

func (s *sink) take(cur arrow.Record) error {
	if cur.NumRows() == 0 {
		cur.Release()
		return nil
	}
	if s.prep.isAgg {
		err := s.agg.Add(cur)
		cur.Release()
		return err
	}
	if s.useTop {
		err := s.top.Add(cur)
		cur.Release()
		return err
	}
	if s.stopEarly {
		need := s.req.Limit - s.kept
		if cur.NumRows() > need {
			trimmed, err := kernel.LimitRecord(cur, need)
			cur.Release()
			if err != nil {
				return err
			}
			cur = trimmed
		}
	}
	s.kept += cur.NumRows()
	s.collected = append(s.collected, cur)
	return nil
}

func (s *sink) finish(schema *arrow.Schema) (arrow.Record, int, error) {
	var out arrow.Record
	var err error
	groups := 0
	switch {
	case s.prep.isAgg:
		out, err = s.agg.Finish()
		if err != nil {
			return nil, 0, err
		}
		groups = s.agg.NumGroups()
		if len(s.req.Order) > 0 {
			sorted, err := kernel.SortRecord(out, s.req.Order)
			out.Release()
			if err != nil {
				return nil, 0, err
			}
			out = sorted
		}
		if s.req.Limit > 0 {
			lim, err := kernel.LimitRecord(out, s.req.Limit)
			out.Release()
			if err != nil {
				return nil, 0, err
			}
			out = lim
		}
	case s.useTop:
		for _, rec := range s.collected {
			if err := s.top.Add(rec); err != nil {
				releaseAll(s.collected)
				s.collected = nil
				return nil, 0, err
			}
			rec.Release()
		}
		s.collected = nil
		out, err = s.top.Finish()
		if err != nil {
			return nil, 0, err
		}
	default:
		if len(s.collected) == 0 {
			if schema == nil {
				return nil, 0, fmt.Errorf("empty scan with no schema")
			}
			out = kernel.EmptyRecord(schema)
		} else if len(s.req.Order) > 0 {
			cat, err := kernel.ConcatRecords(s.collected)
			releaseAll(s.collected)
			s.collected = nil
			if err != nil {
				return nil, 0, err
			}
			sorted, err := kernel.SortRecord(cat, s.req.Order)
			cat.Release()
			if err != nil {
				return nil, 0, err
			}
			out = sorted
			if s.req.Limit > 0 {
				lim, err := kernel.LimitRecord(out, s.req.Limit)
				out.Release()
				if err != nil {
					return nil, 0, err
				}
				out = lim
			}
		} else {
			out, err = kernel.ConcatRecords(s.collected)
			releaseAll(s.collected)
			s.collected = nil
			if err != nil {
				return nil, 0, err
			}
			if s.req.Limit > 0 && out.NumRows() > s.req.Limit {
				lim, err := kernel.LimitRecord(out, s.req.Limit)
				out.Release()
				if err != nil {
					return nil, 0, err
				}
				out = lim
			}
		}
	}
	if len(s.req.Project) > 0 {
		proj, err := kernel.Project(out, s.req.Project)
		out.Release()
		if err != nil {
			return nil, 0, err
		}
		out = proj
	}
	return out, groups, nil
}

func (s *sink) release() {
	releaseAll(s.collected)
	s.collected = nil
}

func runRowSerial(ctx context.Context, req Request, prep prepared) (*Result, error) {
	rdr, err := parquetscan.Open(ctx, req.Table.Files, scanOptions(req, prep))
	if err != nil {
		return nil, err
	}
	defer rdr.Close()
	sink, err := newSink(req, prep)
	if err != nil {
		return nil, err
	}
	for {
		if sink.stopEarly && sink.kept >= req.Limit {
			break
		}
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			sink.release()
			return nil, err
		}
		if err := sink.addRow(rec); err != nil {
			sink.release()
			return nil, err
		}
	}
	out, groups, err := sink.finish(rdr.Schema())
	if err != nil {
		return nil, err
	}
	return &Result{Record: out, Stats: overlayCatalogStats(rdr.Stats(), req), Groups: groups}, nil
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
