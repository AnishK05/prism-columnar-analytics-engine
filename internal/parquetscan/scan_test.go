package parquetscan

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

func tenColSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "c0", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "c1", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "c2", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "c3", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "c4", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "c5", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "c6", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "c7", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "c8", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "c9", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
}

func writeTenColParquet(t *testing.T, path string, rows int, rowGroup int) {
	t.Helper()
	schema := tenColSchema()
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)

	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()

	for i := 0; i < rows; i++ {
		b.Field(0).(*array.Int64Builder).Append(int64(i))
		b.Field(1).(*array.Int64Builder).Append(int64(i * 10))
		b.Field(2).(*array.Int64Builder).Append(int64(i * 100))
		b.Field(3).(*array.Int64Builder).Append(int64(i + 3))
		b.Field(4).(*array.Int64Builder).Append(int64(i + 4))
		b.Field(5).(*array.StringBuilder).Append("alpha")
		b.Field(6).(*array.StringBuilder).Append("bravo")
		b.Field(7).(*array.StringBuilder).Append("charlie")
		b.Field(8).(*array.Int64Builder).Append(int64(i + 8))
		b.Field(9).(*array.Int64Builder).Append(int64(i + 9))
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
		parquet.WithMaxRowGroupLength(int64(rowGroup)),
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

func TestColumnPruningDoesNotMaterializeOtherColumns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.parquet")
	writeTenColParquet(t, path, 4096, 1024)

	full, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.SchemaFields) != 10 {
		t.Fatalf("schema fields = %d, want 10", len(full.SchemaFields))
	}
	if full.NumRowGroups < 2 {
		t.Fatalf("row groups = %d, want >= 2 so stats exist per group", full.NumRowGroups)
	}

	var allBytes int64
	for _, rg := range full.RowGroups {
		allBytes += rg.CompressedBytes
	}

	ctx := context.Background()
	rdr, err := Open(ctx, []string{path}, Options{Columns: []string{"c1", "c5"}, BatchSize: 512})
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Close()

	var rows int64
	var first arrow.Record
	for {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if rec.NumCols() != 2 {
			rec.Release()
			t.Fatalf("record has %d columns, want 2 (pruning failed)", rec.NumCols())
		}
		names := []string{rec.Schema().Field(0).Name, rec.Schema().Field(1).Name}
		if names[0] != "c1" || names[1] != "c5" {
			rec.Release()
			t.Fatalf("columns = %v, want [c1 c5]", names)
		}
		for i := 0; i < int(rec.NumCols()); i++ {
			if rec.Column(i) == nil {
				rec.Release()
				t.Fatal("nil array for a selected column")
			}
		}
		rows += rec.NumRows()
		if first == nil {
			first = rec
		} else {
			rec.Release()
		}
	}
	if first == nil {
		t.Fatal("no records")
	}
	defer first.Release()

	if rows != 4096 {
		t.Fatalf("rows = %d, want 4096", rows)
	}
	st := rdr.Stats()
	if st.ColumnsRead != 2 {
		t.Fatalf("ColumnsRead = %d, want 2", st.ColumnsRead)
	}
	if len(st.ColumnNames) != 2 || st.ColumnNames[0] != "c1" || st.ColumnNames[1] != "c5" {
		t.Fatalf("ColumnNames = %v", st.ColumnNames)
	}
	if st.CompressedBytes <= 0 {
		t.Fatal("expected compressed bytes for selected columns")
	}
	if st.CompressedBytes >= allBytes {
		t.Fatalf("pruned scan read %d compressed bytes, full file is %d; pruning did not reduce IO", st.CompressedBytes, allBytes)
	}
}

func TestUnknownColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.parquet")
	writeTenColParquet(t, path, 16, 16)
	_, err := Open(context.Background(), []string{path}, Options{Columns: []string{"nope"}})
	if err == nil {
		t.Fatal("expected unknown column error")
	}
}

func TestInspectMinMax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.parquet")
	writeTenColParquet(t, path, 100, 50)
	info, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumRows != 100 {
		t.Fatalf("rows = %d", info.NumRows)
	}
	found := false
	for _, rg := range info.RowGroups {
		for _, c := range rg.Columns {
			if c.Name == "c0" && c.HasMinMax {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected min/max stats on c0")
	}
}

func TestScanTwoFiles(t *testing.T) {
	dir := t.TempDir()
	p0 := filepath.Join(dir, "part-0000.parquet")
	p1 := filepath.Join(dir, "part-0001.parquet")
	writeTenColParquet(t, p0, 100, 100)
	writeTenColParquet(t, p1, 50, 50)
	rdr, err := Open(context.Background(), []string{p0, p1}, Options{Columns: []string{"c0"}, BatchSize: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Close()
	var n int64
	for {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n += rec.NumRows()
		rec.Release()
	}
	if n != 150 {
		t.Fatalf("rows = %d, want 150 (scanner stopped after first file?)", n)
	}
}

func TestScanLimitViaConsumer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.parquet")
	writeTenColParquet(t, path, 200, 200)
	rdr, err := Open(context.Background(), []string{path}, Options{Columns: []string{"c0"}, BatchSize: 40})
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Close()
	var n int64
	const limit int64 = 5
	for n < limit {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		take := rec.NumRows()
		if n+take > limit {
			take = limit - n
		}
		n += take
		rec.Release()
	}
	if n != limit {
		t.Fatalf("got %d rows", n)
	}
}

func TestRowGroupSelectionSkipsUnreadGroups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.parquet")
	writeTenColParquet(t, path, 200, 50) // 4 row groups of 50
	info, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumRowGroups < 2 {
		t.Fatalf("row groups = %d", info.NumRowGroups)
	}
	fullBytes := int64(0)
	for _, rg := range info.RowGroups {
		fullBytes += rg.CompressedBytes
	}

	rdr, err := Open(context.Background(), []string{path}, Options{
		Columns:   []string{"c0"},
		BatchSize: 32,
		RowGroups: map[string][]int{path: {0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Close()
	var n int64
	for {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n += rec.NumRows()
		rec.Release()
	}
	if n != 50 {
		t.Fatalf("rows = %d, want 50 (first row group only)", n)
	}
	st := rdr.Stats()
	if st.RowGroupsRead != 1 {
		t.Fatalf("RowGroupsRead = %d, want 1", st.RowGroupsRead)
	}
	if st.RowGroupsSkipped != info.NumRowGroups-1 {
		t.Fatalf("RowGroupsSkipped = %d, want %d", st.RowGroupsSkipped, info.NumRowGroups-1)
	}
	if st.CompressedBytes <= 0 || st.CompressedBytes >= fullBytes {
		t.Fatalf("skipped scan bytes=%d full=%d", st.CompressedBytes, fullBytes)
	}
}
