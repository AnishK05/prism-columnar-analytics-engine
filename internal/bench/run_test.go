package bench

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/engine"
)

func testdataTables(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "tables")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadQueries(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	qs, err := LoadQueries(filepath.Join(root, "bench", "queries.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) < 9 {
		t.Fatalf("got %d queries", len(qs))
	}
	if qs[0].ID != "Q1" || qs[0].SQL == "" {
		t.Fatalf("%+v", qs[0])
	}
}

func TestRunOneTestdata(t *testing.T) {
	dir := testdataTables(t)
	cat, err := catalog.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	q := Query{ID: "Q1", Showcase: "scan-prune", SQL: "SELECT COUNT(*), SUM(amount_cents) FROM events"}
	v := Variant{Name: "vectorized", Engine: engine.KindVectorized}
	run, err := runOne(context.Background(), cat, q, v, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if run.RowsRead != 8192 {
		t.Fatalf("rows_read=%d", run.RowsRead)
	}
	if run.HotMedianMs <= 0 || run.FirstRunMs <= 0 {
		t.Fatalf("timings %+v", run)
	}
	if len(run.RunsMs) != 2 {
		t.Fatalf("runs=%d", len(run.RunsMs))
	}
}

func TestScaleDataDir(t *testing.T) {
	if ScaleDataDir("testdata", "") != filepath.Join("testdata", "tables") {
		t.Fatal(ScaleDataDir("testdata", ""))
	}
	if ScaleDataDir("dev", "x") != "x" {
		t.Fatal("override")
	}
}
