package exec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
	"github.com/apache/arrow-go/v18/arrow/array"
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

func TestScalarCount(t *testing.T) {
	cat, err := catalog.Load(testdataTables(t))
	if err != nil {
		t.Fatal(err)
	}
	tbl, err := cat.Table("events")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Request{
		Table: tbl,
		Aggs:  []kernel.AggSpec{{Fn: kernel.AggCountStar, Name: "count"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	if res.Record.NumRows() != 1 {
		t.Fatal(res.Record.NumRows())
	}
	n := res.Record.Column(0).(*array.Int64).Value(0)
	if n != 8192 {
		t.Fatalf("COUNT(*) = %d, want 8192", n)
	}
}

func TestGroupByCountryMatchesNaive(t *testing.T) {
	cat, err := catalog.Load(testdataTables(t))
	if err != nil {
		t.Fatal(err)
	}
	tbl, _ := cat.Table("events")
	res, err := Run(context.Background(), Request{
		Table:   tbl,
		GroupBy: []string{"country"},
		Aggs: []kernel.AggSpec{
			{Fn: kernel.AggCountStar, Name: "count"},
			{Fn: kernel.AggSum, Input: "amount_cents", Name: "sum_amount_cents"},
		},
		Order: []kernel.OrderKey{{Name: "country"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	naive := naiveGroupCountry(t, tbl)
	if int(res.Record.NumRows()) != len(naive) {
		t.Fatalf("groups %d vs naive %d", res.Record.NumRows(), len(naive))
	}
	cty := res.Record.Column(0).(*array.String)
	cnt := res.Record.Column(1).(*array.Int64)
	sum := res.Record.Column(2).(*array.Int64)
	for i := 0; i < int(res.Record.NumRows()); i++ {
		k := cty.Value(i)
		got := naive[k]
		if cnt.Value(i) != got[0] || sum.Value(i) != got[1] {
			t.Fatalf("%s: got count=%d sum=%d want count=%d sum=%d", k, cnt.Value(i), sum.Value(i), got[0], got[1])
		}
	}
}

func naiveGroupCountry(t *testing.T, tbl *catalog.Table) map[string][2]int64 {
	t.Helper()
	res, err := Run(context.Background(), Request{
		Table:    tbl,
		ScanCols: []string{"country", "amount_cents"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	out := map[string][2]int64{}
	cty := res.Record.Column(0)
	amt := res.Record.Column(1).(*array.Int64)
	for i := 0; i < int(res.Record.NumRows()); i++ {
		k := cty.ValueStr(i)
		g := out[k]
		g[0]++
		if !amt.IsNull(i) {
			g[1] += amt.Value(i)
		}
		out[k] = g
	}
	return out
}

func TestFilterThenAgg(t *testing.T) {
	cat, err := catalog.Load(testdataTables(t))
	if err != nil {
		t.Fatal(err)
	}
	tbl, _ := cat.Table("events")
	pred, err := expr.ParseWhere("amount_cents > 0 AND country = 'US'")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Request{
		Table: tbl,
		Where: pred,
		Aggs:  []kernel.AggSpec{{Fn: kernel.AggCountStar, Name: "count"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	n := res.Record.Column(0).(*array.Int64).Value(0)
	if n != 30 {
		t.Fatalf("filtered COUNT(*) = %d, want 30", n)
	}
}
