package kernel

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func recCountryAmount(countries []string, amounts []int64, validAmt []bool) arrow.Record {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "country", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "amount_cents", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	sb := b.Field(0).(*array.StringBuilder)
	ib := b.Field(1).(*array.Int64Builder)
	for i, c := range countries {
		if c == "" {
			sb.AppendNull()
		} else {
			sb.Append(c)
		}
		if validAmt != nil && !validAmt[i] {
			ib.AppendNull()
		} else {
			ib.Append(amounts[i])
		}
	}
	return b.NewRecord()
}

func TestHashAggGroupCountSum(t *testing.T) {
	rec := recCountryAmount([]string{"US", "CA", "US", "US", "CA"}, []int64{10, 5, 20, 0, 7}, nil)
	defer rec.Release()
	h, err := NewHashAgg([]string{"country"}, []AggSpec{
		{Fn: AggCountStar, Name: "count"},
		{Fn: AggSum, Input: "amount_cents", Name: "sum_amount_cents"},
		{Fn: AggAvg, Input: "amount_cents", Name: "avg_amount_cents"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Add(rec); err != nil {
		t.Fatal(err)
	}
	out, err := h.Finish()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	if out.NumRows() != 2 {
		t.Fatalf("groups = %d", out.NumRows())
	}
	got := map[string][2]int64{}
	cty := out.Column(0).(*array.String)
	cnt := out.Column(1).(*array.Int64)
	sum := out.Column(2).(*array.Int64)
	avg := out.Column(3).(*array.Float64)
	for i := 0; i < int(out.NumRows()); i++ {
		got[cty.Value(i)] = [2]int64{cnt.Value(i), sum.Value(i)}
		if cty.Value(i) == "US" {
			want := (10.0 + 20.0 + 0.0) / 3.0
			if avg.Value(i) != want {
				t.Fatalf("US avg = %v want %v", avg.Value(i), want)
			}
		}
	}
	if got["US"] != [2]int64{3, 30} {
		t.Fatalf("US = %v", got["US"])
	}
	if got["CA"] != [2]int64{2, 12} {
		t.Fatalf("CA = %v", got["CA"])
	}
}

func TestHashAggNullSkipAndScalarCount(t *testing.T) {
	rec := recCountryAmount([]string{"US", "US"}, []int64{10, 0}, []bool{true, false})
	defer rec.Release()
	h, err := NewHashAgg(nil, []AggSpec{
		{Fn: AggCountStar, Name: "count"},
		{Fn: AggCount, Input: "amount_cents", Name: "count_amount"},
		{Fn: AggSum, Input: "amount_cents", Name: "sum_amount_cents"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Add(rec); err != nil {
		t.Fatal(err)
	}
	out, err := h.Finish()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	if out.NumRows() != 1 {
		t.Fatal(out.NumRows())
	}
	if out.Column(0).(*array.Int64).Value(0) != 2 {
		t.Fatal("COUNT(*)")
	}
	if out.Column(1).(*array.Int64).Value(0) != 1 {
		t.Fatal("COUNT(amount) should skip null")
	}
	if out.Column(2).(*array.Int64).Value(0) != 10 {
		t.Fatal("SUM skip null")
	}
}

func TestHashAggEmptyScalar(t *testing.T) {
	h, err := NewHashAgg(nil, []AggSpec{{Fn: AggCountStar, Name: "count"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Finish()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	if out.NumRows() != 1 || out.Column(0).(*array.Int64).Value(0) != 0 {
		t.Fatal("empty COUNT(*) should be 0")
	}
}

func TestParseAggList(t *testing.T) {
	specs, err := ParseAggList("count,sum(amount_cents),avg(qty)")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 3 || specs[0].Fn != AggCountStar || specs[1].Input != "amount_cents" {
		t.Fatalf("%+v", specs)
	}
}

func TestSortAndLimit(t *testing.T) {
	rec := recCountryAmount([]string{"US", "CA", "GB"}, []int64{3, 1, 2}, nil)
	defer rec.Release()
	sorted, err := SortRecord(rec, []OrderKey{{Name: "amount_cents", Desc: false}})
	if err != nil {
		t.Fatal(err)
	}
	defer sorted.Release()
	got := sorted.Column(1).(*array.Int64)
	if got.Value(0) != 1 || got.Value(1) != 2 || got.Value(2) != 3 {
		t.Fatal("sort")
	}
	lim, err := LimitRecord(sorted, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer lim.Release()
	if lim.NumRows() != 2 {
		t.Fatal(lim.NumRows())
	}
}

func TestTopN(t *testing.T) {
	rec := recCountryAmount([]string{"US", "CA", "GB", "IN"}, []int64{30, 10, 20, 40}, nil)
	defer rec.Release()
	tn, err := NewTopN(2, []OrderKey{{Name: "amount_cents", Desc: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tn.Add(rec); err != nil {
		t.Fatal(err)
	}
	out, err := tn.Finish()
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	if out.NumRows() != 2 {
		t.Fatal(out.NumRows())
	}
	a := out.Column(1).(*array.Int64)
	if a.Value(0) != 40 || a.Value(1) != 30 {
		t.Fatalf("top-n desc got %d %d", a.Value(0), a.Value(1))
	}
}

func TestSortNullsLastASC(t *testing.T) {
	rec := recCountryAmount([]string{"US", "CA", "GB"}, []int64{2, 0, 1}, []bool{true, false, true})
	defer rec.Release()
	sorted, err := SortRecord(rec, []OrderKey{{Name: "amount_cents"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sorted.Release()
	a := sorted.Column(1).(*array.Int64)
	if a.IsNull(2) == false || a.Value(0) != 1 || a.Value(1) != 2 {
		t.Fatalf("nulls last: %v %v %v", a.Value(0), a.Value(1), a.IsNull(2))
	}
}
