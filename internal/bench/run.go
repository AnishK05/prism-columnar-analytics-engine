package bench

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/engine"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/version"
)

// Variant is one cell in the §9.2 breakdown.
type Variant struct {
	Name    string      `json:"name"`
	Engine  engine.Kind `json:"engine"`
	NoSkip  bool        `json:"no_skip"`
	NoPrune bool        `json:"no_prune"`
}

// QueryRun is timings + scan stats for one (query, variant).
type QueryRun struct {
	ID            string    `json:"id"`
	Showcase      string    `json:"showcase"`
	Variant       string    `json:"variant"`
	Engine        string    `json:"engine"`
	NoSkip        bool      `json:"no_skip"`
	NoPrune       bool      `json:"no_prune"`
	WarmupMs      float64   `json:"warmup_ms"`
	RunsMs        []float64 `json:"runs_ms"`
	FirstRunMs    float64   `json:"first_run_ms"`
	HotMedianMs   float64   `json:"hot_median_ms"`
	HotP95Ms      float64   `json:"hot_p95_ms"`
	RowsRead      int64     `json:"rows_read"`
	RowsEmitted   int64     `json:"rows_emitted"`
	BytesRead     int64     `json:"bytes_read"`
	RowGroupsRead int       `json:"row_groups_read"`
	RowGroupsSkip int       `json:"row_groups_skipped"`
	ColumnsRead   int       `json:"columns_read"`
	Jobs          int       `json:"jobs"`
	PeakRSSBytes  int64     `json:"peak_rss_bytes"`
	GoMemSysBytes uint64    `json:"go_mem_sys_bytes"`
}

// Report is the checked-in / UI JSON document.
type Report struct {
	Schema      string     `json:"schema"`
	Note        string     `json:"note"`
	Scale       string     `json:"scale"`
	DataDir     string     `json:"data_dir"`
	Rows        int64      `json:"rows"`
	Repeat      int        `json:"repeat"`
	Jobs        int        `json:"jobs"`
	Version     string     `json:"version"`
	GitSHA      string     `json:"git_sha,omitempty"`
	GeneratedAt string     `json:"generated_at"`
	Hardware    Hardware   `json:"hardware"`
	Variants    []Variant  `json:"variants"`
	Results     []QueryRun `json:"results"`
	Speedups    []Speedup  `json:"speedups,omitempty"`
}

// Speedup is vectorized+opt vs naive row (no skip/prune) on hot median.
type Speedup struct {
	ID       string  `json:"id"`
	NaiveMs  float64 `json:"naive_hot_median_ms"`
	VecMs    float64 `json:"vectorized_hot_median_ms"`
	SpeedupX float64 `json:"speedup_x"`
}

const sampleNote = "Checked-in / local sample for the workbench UI. " +
	"Not a laptop-scale measurement. Do not copy these timings onto the resume. " +
	"Warmup is discarded; first_run_ms is measured run 1; hot_* uses runs 2–N."

func defaultVariants(engineFlag string, breakdown bool) ([]Variant, error) {
	all := []Variant{
		{Name: "row-naive", Engine: engine.KindRow, NoSkip: true, NoPrune: true},
		{Name: "row-opt", Engine: engine.KindRow, NoSkip: false, NoPrune: false},
		{Name: "vectorized", Engine: engine.KindVectorized, NoSkip: false, NoPrune: false},
	}
	switch strings.ToLower(strings.TrimSpace(engineFlag)) {
	case "", "all":
		if breakdown {
			return all, nil
		}
		return []Variant{all[2]}, nil
	case "vectorized", "vec":
		return []Variant{all[2]}, nil
	case "row", "row-at-a-time", "naive":
		if breakdown {
			return all[:2], nil
		}
		return []Variant{all[1]}, nil
	default:
		return nil, fmt.Errorf("unknown --engine %q (want all, vectorized, row)", engineFlag)
	}
}

// Main is the `prism bench` / `go run ./bench` entry point.
func Main(args []string) error {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	scale := fs.String("scale", "testdata", "testdata|tiny|dev|laptop")
	dataDir := fs.String("data-dir", "", "tables root (default from --scale)")
	engineFlag := fs.String("engine", "all", "all|vectorized|row")
	repeat := fs.Int("repeat", 5, "measured runs after one warmup")
	jobs := fs.Int("jobs", 0, "parallel workers (0 = PRISM_PARALLELISM or GOMAXPROCS)")
	queriesPath := fs.String("queries", filepath.Join("bench", "queries.json"), "query catalog JSON")
	outPath := fs.String("out", "", "write JSON report (default stdout only)")
	breakdown := fs.Bool("breakdown", true, "3-way §9.2 breakdown when --engine=all")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *repeat < 1 {
		return fmt.Errorf("--repeat must be >= 1")
	}
	variants, err := defaultVariants(*engineFlag, *breakdown)
	if err != nil {
		return err
	}
	queries, err := LoadQueries(*queriesPath)
	if err != nil {
		return err
	}
	dir := catalog.ResolveDataDir(ScaleDataDir(*scale, *dataDir))
	cat, err := catalog.Load(dir)
	if err != nil {
		return err
	}
	tbl, err := cat.Table("events")
	if err != nil {
		return fmt.Errorf("bench scale %s: %w (generate with: py -3 scripts/generate_data.py --scale %s)", *scale, err, *scale)
	}

	rep := Report{
		Schema:      "prism-bench-v1",
		Note:        sampleNote,
		Scale:       *scale,
		DataDir:     dir,
		Rows:        tbl.NumRows,
		Repeat:      *repeat,
		Jobs:        engine.ResolveJobs(*jobs),
		Version:     version.Version,
		GitSHA:      gitSHA(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Hardware:    CaptureHardware(),
		Variants:    variants,
	}

	ctx := context.Background()
	fmt.Fprintf(os.Stderr, "prism bench scale=%s rows=%d engine=%s repeat=%d jobs=%d variants=%d\n",
		*scale, tbl.NumRows, *engineFlag, *repeat, rep.Jobs, len(variants))

	for _, q := range queries {
		for _, v := range variants {
			run, err := runOne(ctx, cat, q, v, *jobs, *repeat)
			if err != nil {
				return fmt.Errorf("%s %s: %w", q.ID, v.Name, err)
			}
			rep.Results = append(rep.Results, run)
			fmt.Fprintf(os.Stderr, "  %s %-12s first=%.2fms hot_median=%.2fms hot_p95=%.2fms rows_read=%d skipped=%d cols=%d\n",
				run.ID, run.Variant, run.FirstRunMs, run.HotMedianMs, run.HotP95Ms, run.RowsRead, run.RowGroupsSkip, run.ColumnsRead)
		}
	}
	rep.Speedups = speedups(rep.Results)
	printTable(rep)
	if *outPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil && filepath.Dir(*outPath) != "." {
			return err
		}
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*outPath, append(b, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
	}
	return nil
}

func runOne(ctx context.Context, cat *catalog.Catalog, q Query, v Variant, jobs, repeat int) (QueryRun, error) {
	opts := engine.Opts{
		Catalog: cat,
		Engine:  v.Engine,
		Jobs:    jobs,
		NoSkip:  v.NoSkip,
		NoPrune: v.NoPrune,
	}
	warmup, err := timedRun(ctx, q.SQL, opts)
	if err != nil {
		return QueryRun{}, fmt.Errorf("warmup: %w", err)
	}
	measured := make([]float64, 0, repeat)
	var last engine.Profile
	for i := 0; i < repeat; i++ {
		p, err := timedRun(ctx, q.SQL, opts)
		if err != nil {
			return QueryRun{}, fmt.Errorf("run %d: %w", i+1, err)
		}
		measured = append(measured, p.ElapsedMs)
		last = p
	}
	first, med, p95 := Summarize(measured)
	return QueryRun{
		ID:            q.ID,
		Showcase:      q.Showcase,
		Variant:       v.Name,
		Engine:        string(v.Engine),
		NoSkip:        v.NoSkip,
		NoPrune:       v.NoPrune,
		WarmupMs:      warmup.ElapsedMs,
		RunsMs:        measured,
		FirstRunMs:    first,
		HotMedianMs:   med,
		HotP95Ms:      p95,
		RowsRead:      last.RowsRead,
		RowsEmitted:   last.RowsEmitted,
		BytesRead:     last.BytesRead,
		RowGroupsRead: last.RowGroupsRead,
		RowGroupsSkip: last.RowGroupsSkipped,
		ColumnsRead:   last.ColumnsRead,
		Jobs:          last.Jobs,
		PeakRSSBytes:  PeakRSSBytes(),
		GoMemSysBytes: GoMemSysBytes(),
	}, nil
}

func timedRun(ctx context.Context, sqlText string, opts engine.Opts) (engine.Profile, error) {
	res, err := engine.Run(ctx, sqlText, opts)
	if err != nil {
		return engine.Profile{}, err
	}
	res.Record.Release()
	return res.Profile, nil
}

func speedups(runs []QueryRun) []Speedup {
	naive := map[string]float64{}
	vec := map[string]float64{}
	order := []string{}
	seen := map[string]bool{}
	for _, r := range runs {
		if !seen[r.ID] {
			order = append(order, r.ID)
			seen[r.ID] = true
		}
		switch r.Variant {
		case "row-naive":
			naive[r.ID] = r.HotMedianMs
		case "vectorized":
			vec[r.ID] = r.HotMedianMs
		}
	}
	var out []Speedup
	for _, id := range order {
		n, okN := naive[id]
		v, okV := vec[id]
		if !okN || !okV || v <= 0 {
			continue
		}
		out = append(out, Speedup{ID: id, NaiveMs: n, VecMs: v, SpeedupX: n / v})
	}
	return out
}

func printTable(rep Report) {
	fmt.Printf("\nPrismBench  scale=%s  rows=%d  commit=%s  prism=%s\n", rep.Scale, rep.Rows, shortSHA(rep.GitSHA), rep.Version)
	fmt.Printf("protocol: 1 warmup discarded; first_run = measured[0]; hot = median/p95 of measured[1:]\n")
	fmt.Printf("hardware: %s/%s cpus=%d mem=%s\n\n",
		rep.Hardware.OS, rep.Hardware.Arch, rep.Hardware.NumCPU, formatBytes(int64(rep.Hardware.MemBytes)))
	fmt.Printf("%-8s %-12s %10s %12s %10s %10s %8s %8s\n",
		"query", "variant", "first_ms", "hot_median", "hot_p95", "rows_read", "skipped", "cols")
	for _, r := range rep.Results {
		fmt.Printf("%-8s %-12s %10.2f %12.2f %10.2f %10d %8d %8d\n",
			r.ID, r.Variant, r.FirstRunMs, r.HotMedianMs, r.HotP95Ms, r.RowsRead, r.RowGroupsSkip, r.ColumnsRead)
	}
	if len(rep.Speedups) > 0 {
		fmt.Printf("\nSpeedup  vectorized+opt vs row-naive (hot median)\n")
		for _, s := range rep.Speedups {
			fmt.Printf("  %s  %.2fx  (%.2fms / %.2fms)\n", s.ID, s.SpeedupX, s.NaiveMs, s.VecMs)
		}
	}
	fmt.Println()
}

func gitSHA() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	if s == "" {
		return "-"
	}
	return s
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "?"
	}
	const gi = 1024 * 1024 * 1024
	if n >= gi {
		return fmt.Sprintf("%.1f GiB", float64(n)/float64(gi))
	}
	const mi = 1024 * 1024
	return fmt.Sprintf("%.0f MiB", float64(n)/float64(mi))
}
