package bench

import "testing"

func TestMedianP95(t *testing.T) {
	if Median(nil) != 0 {
		t.Fatal("empty")
	}
	if Median([]float64{3}) != 3 {
		t.Fatal("one")
	}
	if Median([]float64{1, 2, 3, 4, 5}) != 3 {
		t.Fatal("odd")
	}
	if Median([]float64{1, 2, 3, 4}) != 2.5 {
		t.Fatal("even")
	}
	p := P95([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	if p != 19 {
		t.Fatalf("p95=%v want 19", p)
	}
}

func TestSummarize(t *testing.T) {
	first, med, p95 := Summarize([]float64{10, 4, 6, 8, 5})
	if first != 10 {
		t.Fatalf("first=%v", first)
	}
	if med != 5.5 {
		t.Fatalf("hot median=%v", med)
	}
	if p95 != 8 {
		t.Fatalf("hot p95=%v", p95)
	}
	f, m, p := Summarize([]float64{7})
	if f != 7 || m != 7 || p != 7 {
		t.Fatalf("single %v %v %v", f, m, p)
	}
}
