package kernel

import (
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
)

func TestKeepRowMatchesEvalMask(t *testing.T) {
	rec := int64Col("amount_cents", []int64{0, 10, 20}, []bool{true, true, false})
	defer rec.Release()
	pred, err := expr.ParseWhere("amount_cents > 5")
	if err != nil {
		t.Fatal(err)
	}
	mask, err := Eval(pred, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer mask.Release()
	for i := 0; i < int(rec.NumRows()); i++ {
		keep, err := KeepRow(pred, rec, i)
		if err != nil {
			t.Fatal(err)
		}
		want := !mask.IsNull(i) && mask.Value(i)
		if keep != want {
			t.Errorf("row %d: KeepRow=%v mask true=%v null=%v", i, keep, !mask.IsNull(i) && mask.Value(i), mask.IsNull(i))
		}
	}
}
