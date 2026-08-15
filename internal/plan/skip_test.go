package plan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/exec"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

func TestSkipRules(t *testing.T) {
	rg := catalog.RowGroup{
		Columns: []catalog.ColStats{{
			Name:      "x",
			HasMinMax: true,
			MinBound:  parquetscan.Bound{Kind: parquetscan.BoundInt64, I64: 10},
			MaxBound:  parquetscan.Bound{Kind: parquetscan.BoundInt64, I64: 20},
			NumValues: 100,
		}},
	}
	cases := []struct {
		src  string
		skip bool
	}{
		{"x > 20", true},
		{"x > 19", false},
		{"x >= 21", true},
		{"x >= 20", false},
		{"x < 10", true},
		{"x < 11", false},
		{"x <= 9", true},
		{"x = 5", true},
		{"x = 15", false},
		{"x = 25", true},
		{"x IN (1, 2)", true},
		{"x IN (15, 100)", false},
		{"x BETWEEN 1 AND 5", true},
		{"x BETWEEN 15 AND 18", false},
		{"x > 20 AND x < 100", true},
		{"x > 20 OR x < 5", true},
		{"x > 20 OR x = 15", false},
	}
	for _, tc := range cases {
		pred, err := expr.ParseWhere(tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		got := canSkip(rg, pred)
		if got != tc.skip {
			t.Errorf("%s: skip=%v want %v", tc.src, got, tc.skip)
		}
	}
}

func TestSkip2020RowGroup(t *testing.T) {
	dir := t.TempDir()
	tableDir := filepath.Join(dir, "events")
	if err := os.MkdirAll(tableDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tableDir, "part-0000.parquet")
	writeTwoEra(t, path, 100)

	cat, err := catalog.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := cat.Table("events")
	if err != nil {
		t.Fatal(err)
	}
	if tbl.NumRowGroups < 2 {
		t.Fatalf("row groups = %d, need 2", tbl.NumRowGroups)
	}
	pred, err := expr.ParseWhere("ts >= 1704067200000") // 2024-01-01
	if err != nil {
		t.Fatal(err)
	}
	n := Build(Input{
		Table:    tbl,
		Where:    pred,
		ScanCols: []string{"ts", "v"},
		Project:  []string{"ts", "v"},
	})
	scan := n.find(OpScan)
	if scan.RowGroupsSkipped < 1 {
		t.Fatalf("expected to skip the 2020 group, kept=%d skipped=%d\n%s",
			scan.RowGroupsKept, scan.RowGroupsSkipped, n.Text())
	}
	if scan.RowGroupsKept != 1 {
		t.Fatalf("kept=%d want 1", scan.RowGroupsKept)
	}

	res, err := exec.Run(context.Background(), n.Request(64))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	if res.Record.NumRows() != 100 {
		t.Fatalf("rows = %d, want 100 (2024 group only)", res.Record.NumRows())
	}
	if res.Stats.RowGroupsSkipped < 1 {
		t.Fatalf("reader skip hook: skipped=%d read=%d total=%d",
			res.Stats.RowGroupsSkipped, res.Stats.RowGroupsRead, res.Stats.RowGroupsTotal)
	}
	if res.Stats.RowGroupsRead != 1 {
		t.Fatalf("read %d row groups, want 1", res.Stats.RowGroupsRead)
	}
}

func writeTwoEra(t *testing.T, path string, perGroup int) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}, Nullable: true},
		{Name: "v", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	tb := b.Field(0).(*array.TimestampBuilder)
	vb := b.Field(1).(*array.Int64Builder)
	const ts2020 int64 = 1577836800000 // 2020-01-01
	const ts2024 int64 = 1717200000000 // 2024-06-01
	for i := 0; i < perGroup; i++ {
		tb.Append(arrow.Timestamp(ts2020))
		vb.Append(int64(i))
	}
	for i := 0; i < perGroup; i++ {
		tb.Append(arrow.Timestamp(ts2024))
		vb.Append(int64(1000 + i))
	}
	rec := b.NewRecord()
	defer rec.Release()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	props := parquet.NewWriterProperties(
		parquet.WithCompression(compress.Codecs.Zstd),
		parquet.WithMaxRowGroupLength(int64(perGroup)),
		parquet.WithStats(true),
	)
	w, err := pqarrow.NewFileWriter(schema, f, props, pqarrow.DefaultWriterProps())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
