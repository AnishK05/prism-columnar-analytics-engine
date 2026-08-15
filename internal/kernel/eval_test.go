package kernel

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func int64Col(name string, vals []int64, valid []bool) arrow.Record {
	schema := arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	ib := b.Field(0).(*array.Int64Builder)
	for i, v := range vals {
		if valid != nil && !valid[i] {
			ib.AppendNull()
			continue
		}
		ib.Append(v)
	}
	return b.NewRecord()
}

func maskBits(t *testing.T, mask *array.Boolean) (trues int, nulls int, falses int) {
	t.Helper()
	for i := 0; i < mask.Len(); i++ {
		if mask.IsNull(i) {
			nulls++
			continue
		}
		if mask.Value(i) {
			trues++
		} else {
			falses++
		}
	}
	return
}

func TestNullCompareIsUnknown(t *testing.T) {
	rec := int64Col("amount_cents", []int64{1, 0, -1, 5}, []bool{true, false, true, true})
	defer rec.Release()
	e, err := expr.ParseWhere("amount_cents > 0")
	if err != nil {
		t.Fatal(err)
	}
	mask, err := Eval(e, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer mask.Release()
	trues, nulls, falses := maskBits(t, mask)
	if trues != 2 || nulls != 1 || falses != 1 {
		t.Fatalf("true=%d null=%d false=%d (NULL > 0 must be unknown, not true)", trues, nulls, falses)
	}
	out, err := Compact(rec, mask)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	if out.NumRows() != 2 {
		t.Fatalf("compacted rows = %d, want 2", out.NumRows())
	}
	got := out.Column(0).(*array.Int64)
	if got.Value(0) != 1 || got.Value(1) != 5 {
		t.Fatalf("got %v, %v", got.Value(0), got.Value(1))
	}
}

func TestAndOrNotEmptyAllTrue(t *testing.T) {
	rec := int64Col("x", []int64{1, 2, 3}, nil)
	defer rec.Release()

	// empty batch
	empty := int64Col("x", nil, nil)
	defer empty.Release()
	e, _ := expr.ParseWhere("x > 0")
	m, err := Eval(e, empty)
	if err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 {
		t.Fatal("empty")
	}
	m.Release()

	all, err := Eval(e, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer all.Release()
	trues, _, _ := maskBits(t, all)
	if trues != 3 {
		t.Fatalf("all-true %d", trues)
	}

	none, err := Eval(mustParse(t, "x < 0"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer none.Release()
	trues, _, falses := maskBits(t, none)
	if trues != 0 || falses != 3 {
		t.Fatalf("all-false t=%d f=%d", trues, falses)
	}

	andE := mustParse(t, "x > 1 AND x < 3")
	andM, err := Eval(andE, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer andM.Release()
	if CountTrue(andM) != 1 {
		t.Fatal(CountTrue(andM))
	}

	notE := mustParse(t, "NOT x = 2")
	notM, err := Eval(notE, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer notM.Release()
	if CountTrue(notM) != 2 {
		t.Fatal(CountTrue(notM))
	}
}

func TestInAndIsNull(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "country", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	sb := b.Field(0).(*array.StringBuilder)
	sb.Append("US")
	sb.AppendNull()
	sb.Append("CA")
	sb.Append("GB")
	rec := b.NewRecord()
	defer rec.Release()

	inM, err := Eval(mustParse(t, "country IN ('US', 'GB')"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer inM.Release()
	if CountTrue(inM) != 2 {
		t.Fatalf("IN true=%d", CountTrue(inM))
	}
	_, nulls, _ := maskBits(t, inM)
	if nulls != 1 {
		t.Fatalf("NULL IN list should be unknown, nulls=%d", nulls)
	}

	isM, err := Eval(mustParse(t, "country IS NULL"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer isM.Release()
	if CountTrue(isM) != 1 {
		t.Fatal(CountTrue(isM))
	}
}

func TestFilterTestdataMatchesNaive(t *testing.T) {
	dir := testdataTables(t)
	files, err := filepath.Glob(filepath.Join(dir, "events", "*.parquet"))
	if err != nil || len(files) == 0 {
		t.Fatal(err)
	}
	pred := mustParse(t, "amount_cents > 0 AND country = 'US'")
	rdr, err := parquetscan.Open(context.Background(), files, parquetscan.Options{
		Columns:   []string{"amount_cents", "country"},
		BatchSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rdr.Close()
	var vec, naive int64
	for {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		mask, err := Eval(pred, rec)
		if err != nil {
			rec.Release()
			t.Fatal(err)
		}
		vec += CountTrue(mask)
		naive += naiveCount(rec, pred)
		mask.Release()
		rec.Release()
	}
	if vec != naive {
		t.Fatalf("vectorized %d vs naive %d", vec, naive)
	}
	// Seed-42 testdata fixture; scripts/filter_oracle.py must print the same count.
	const want int64 = 30
	if vec != want {
		t.Fatalf("matching rows = %d, want %d (pyarrow compute oracle)", vec, want)
	}
}

func TestBetweenAndTimestamp(t *testing.T) {
	rec := int64Col("amount_cents", []int64{0, 5, 10, 15}, []bool{true, true, false, true})
	defer rec.Release()
	m, err := Eval(mustParse(t, "amount_cents BETWEEN 5 AND 10"), rec)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Release()
	trues, nulls, _ := maskBits(t, m)
	if trues != 1 || nulls != 1 {
		t.Fatalf("BETWEEN true=%d null=%d (NULL BETWEEN is unknown)", trues, nulls)
	}

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "ts", Type: &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"}, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	tb := b.Field(0).(*array.TimestampBuilder)
	tb.Append(arrow.Timestamp(1704067200000))
	tb.AppendNull()
	tb.Append(arrow.Timestamp(1735603199000))
	tsRec := b.NewRecord()
	defer tsRec.Release()
	tsM, err := Eval(mustParse(t, "ts >= 1704067200000"), tsRec)
	if err != nil {
		t.Fatal(err)
	}
	defer tsM.Release()
	trues, nulls, _ = maskBits(t, tsM)
	if trues != 2 || nulls != 1 {
		t.Fatalf("ts compare true=%d null=%d", trues, nulls)
	}
}

func naiveCount(rec arrow.Record, e expr.Expr) int64 {
	// Independent row loop: keep only where the predicate is TRUE (not unknown).
	// country may be dictionary-encoded in the fixture.
	amt := rec.Column(0).(*array.Int64)
	cty := rec.Column(1)
	var n int64
	for i := 0; i < int(rec.NumRows()); i++ {
		if amt.IsNull(i) || cty.IsNull(i) {
			continue // unknown AND anything that's not false → for this pred, unknown
		}
		if amt.Value(i) > 0 && cty.ValueStr(i) == "US" {
			n++
		}
	}
	return n
}

func mustParse(t *testing.T, src string) expr.Expr {
	t.Helper()
	e, err := expr.ParseWhere(src)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

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
