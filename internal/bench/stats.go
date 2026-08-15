package bench

import (
	"math"
	"sort"
)

// Median returns the median of xs. Empty input is 0.
func Median(xs []float64) float64 {
	s := sortedCopy(xs)
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// P95 is a nearest-rank 95th percentile.
func P95(xs []float64) float64 {
	s := sortedCopy(xs)
	n := len(s)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(0.95*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return s[idx]
}

func sortedCopy(xs []float64) []float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s
}

// Summarize splits measured runs into first-run vs hot-cache (runs 2–N).
// Warmup is not included in measured.
func Summarize(measured []float64) (first, hotMedian, hotP95 float64) {
	if len(measured) == 0 {
		return 0, 0, 0
	}
	first = measured[0]
	hot := measured
	if len(measured) > 1 {
		hot = measured[1:]
	}
	return first, Median(hot), P95(hot)
}
