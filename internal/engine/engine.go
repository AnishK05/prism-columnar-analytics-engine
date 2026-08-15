// Package engine wires SQL → plan → vectorized or row-at-a-time execution.
package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/exec"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/plan"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/sql"
	"github.com/apache/arrow-go/v18/arrow"
)

const (
	KindVectorized Kind = "vectorized"
	KindRow        Kind = "row"
)

// Kind is the execution backend.
type Kind string

// Opts control engine.Run.
type Opts struct {
	DataDir     string
	Catalog     *catalog.Catalog
	Engine      Kind
	Jobs        int // 0 = PRISM_PARALLELISM or GOMAXPROCS
	BatchSize   int64
	NoSkip      bool
	NoPrune     bool
	ReturnLimit int64 // extra cap on returned rows (0 = no extra cap)
}

// Profile is the UI/HTTP timing and skip payload.
type Profile struct {
	ElapsedMs        float64 `json:"elapsed_ms"`
	Engine           string  `json:"engine"`
	Jobs             int     `json:"jobs"`
	RowsRead         int64   `json:"rows_read"`
	RowsEmitted      int64   `json:"rows_emitted"`
	BytesRead        int64   `json:"bytes_read"`
	RowGroupsTotal   int     `json:"row_groups_total"`
	RowGroupsRead    int     `json:"row_groups_read"`
	RowGroupsSkipped int     `json:"row_groups_skipped"`
	ColumnsRead      int     `json:"columns_read"`
	Plan             any     `json:"plan,omitempty"`
}

// Result is an Arrow table plus profile. Caller must Release Record.
type Result struct {
	Record    arrow.Record
	Profile   Profile
	Node      *plan.Node
	Groups    int
	Truncated bool
}

// ParseKind accepts vectorized|row.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "vectorized", "vec":
		return KindVectorized, nil
	case "row", "row-at-a-time", "naive":
		return KindRow, nil
	default:
		return "", fmt.Errorf("unknown engine %q (want vectorized or row)", s)
	}
}

// ResolveJobs maps 0 to PRISM_PARALLELISM or GOMAXPROCS.
func ResolveJobs(n int) int {
	if n > 0 {
		return n
	}
	if s := os.Getenv("PRISM_PARALLELISM"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			return v
		}
	}
	p := runtime.GOMAXPROCS(0)
	if p < 1 {
		return 1
	}
	return p
}

// Run parses and executes SQL.
func Run(ctx context.Context, src string, opts Opts) (*Result, error) {
	q, err := sql.Parse(src)
	if err != nil {
		return nil, err
	}
	cat := opts.Catalog
	if cat == nil {
		cat, err = catalog.Load(catalog.ResolveDataDir(opts.DataDir))
		if err != nil {
			return nil, err
		}
	}
	bound, err := sql.Bind(q, cat)
	if err != nil {
		return nil, err
	}
	return RunInput(ctx, plan.Input{
		Table:    bound.Table,
		Where:    bound.Where,
		ScanCols: bound.ScanCols,
		GroupBy:  bound.GroupBy,
		Aggs:     bound.Aggs,
		Project:  bound.Project,
		Order:    bound.Order,
		Limit:    bound.Limit,
		IsAgg:    bound.IsAgg,
		NoSkip:   opts.NoSkip,
		NoPrune:  opts.NoPrune,
	}, opts)
}

// RunInput executes a bound plan input (SQL or flag-based agg).
func RunInput(ctx context.Context, in plan.Input, opts Opts) (*Result, error) {
	kind := opts.Engine
	if kind == "" {
		kind = KindVectorized
	}
	jobs := ResolveJobs(opts.Jobs)
	batch := opts.BatchSize
	if batch <= 0 {
		batch = parquetscan.DefaultBatchSize
	}
	node := plan.Build(in)
	node.SetJobs(jobs)
	req := node.Request(batch)
	req.Jobs = jobs
	if kind == KindRow {
		req.Engine = exec.EngineRow
	} else {
		req.Engine = exec.EngineVectorized
	}

	start := time.Now()
	res, err := exec.Run(ctx, req)
	elapsed := time.Since(start)
	if err != nil {
		return nil, err
	}
	node.AttachStats(res.Stats)
	out := res.Record
	truncated := false
	if opts.ReturnLimit > 0 && out != nil && out.NumRows() > opts.ReturnLimit {
		lim, err := kernel.LimitRecord(out, opts.ReturnLimit)
		out.Release()
		if err != nil {
			return nil, err
		}
		out = lim
		truncated = true
	}
	st := res.Stats
	return &Result{
		Record:    out,
		Node:      node,
		Groups:    res.Groups,
		Truncated: truncated,
		Profile: Profile{
			ElapsedMs:        float64(elapsed.Microseconds()) / 1000.0,
			Engine:           string(kind),
			Jobs:             jobs,
			RowsRead:         st.RowsRead,
			RowsEmitted:      numRows(out),
			BytesRead:        st.CompressedBytes,
			RowGroupsTotal:   st.RowGroupsTotal,
			RowGroupsRead:    st.RowGroupsRead,
			RowGroupsSkipped: st.RowGroupsSkipped,
			ColumnsRead:      st.ColumnsRead,
			Plan:             node.JSON(),
		},
	}, nil
}

func numRows(rec arrow.Record) int64 {
	if rec == nil {
		return 0
	}
	return rec.NumRows()
}
