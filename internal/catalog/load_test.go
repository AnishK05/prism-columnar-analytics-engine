package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

func testdataTables(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "tables")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("testdata not found at %s: %v", dir, err)
	}
	return dir
}

func TestLoadTestdata(t *testing.T) {
	cat, err := Load(testdataTables(t))
	if err != nil {
		t.Fatal(err)
	}
	names := cat.Names()
	if len(names) != 3 {
		t.Fatalf("tables = %v, want events, products, users", names)
	}
	events, err := cat.Table("events")
	if err != nil {
		t.Fatal(err)
	}
	if events.NumRows != 8192 {
		t.Fatalf("events rows = %d, want 8192 (generator manifest)", events.NumRows)
	}
	if events.NumRowGroups != 4 {
		t.Fatalf("row groups = %d, want 4", events.NumRowGroups)
	}
	if len(events.Fields) != 10 {
		t.Fatalf("fields = %d", len(events.Fields))
	}
	ok, detail := events.Clustering("ts")
	if !ok {
		t.Fatalf("ts should be clustered: %s", detail)
	}
	t.Log("ts clustering:", detail)

	// min of first rg should be 2024-01-01, last max end of 2024
	first, _ := events.RowGroups[0].ColStats("ts")
	last, _ := events.RowGroups[len(events.RowGroups)-1].ColStats("ts")
	if !first.HasMinMax || first.MinBound.I64 != 1704067200000 {
		t.Fatalf("first ts min = %+v", first.MinBound)
	}
	if last.MaxBound.I64 != 1735603199000 {
		t.Fatalf("last ts max = %+v", last.MaxBound)
	}
	minTS, maxTS, ok := events.TimestampRangeMS("ts")
	if !ok || minTS != 1704067200000 || maxTS != 1735603199000 {
		t.Fatalf("TimestampRangeMS %d %d ok=%v", minTS, maxTS, ok)
	}
	info := events.Info()
	if info.Rows != 8192 || info.Files != 2 || info.MinTSMs == nil || *info.MinTSMs != minTS {
		t.Fatalf("Info %+v", info)
	}
}

func TestLoadMissingDir(t *testing.T) {
	cat, err := Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Names()) != 0 {
		t.Fatalf("expected empty catalog, got %v", cat.Names())
	}
}

func TestSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	table := filepath.Join(dir, "bad")
	if err := os.MkdirAll(table, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTiny(t, filepath.Join(table, "a.parquet"), "x")
	writeTiny(t, filepath.Join(table, "b.parquet"), "y")
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected schema mismatch")
	}
}

func writeTiny(t *testing.T, path, col string) {
	t.Helper()
	schema := arrow.NewSchema([]arrow.Field{{Name: col, Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	b.Field(0).(*array.Int64Builder).Append(1)
	rec := b.NewRecord()
	defer rec.Release()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := pqarrow.NewFileWriter(schema, f, parquet.NewWriterProperties(parquet.WithCompression(compress.Codecs.Zstd), parquet.WithStats(true)), pqarrow.DefaultWriterProps())
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
