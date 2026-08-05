// Package benchstat provides small helpers for create-bench aggregation.
package benchstat

import "sort"

// PercentileNearestRank returns the nearest-rank percentile p (0–100) of vals.
// vals are sorted in place. Empty input returns 0.
func PercentileNearestRank(vals []int64, p float64) int64 {
	n := len(vals)
	if n == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	if n == 1 {
		return vals[0]
	}
	if p <= 0 {
		return vals[0]
	}
	if p >= 100 {
		return vals[n-1]
	}
	// nearest-rank: index = round(p/100 * (n-1))
	k := int(p/100.0*float64(n-1) + 0.5)
	if k < 0 {
		k = 0
	}
	if k >= n {
		k = n - 1
	}
	return vals[k]
}

// Summary holds min/max/avg/p50/p95 for a sample set (milliseconds).
type Summary struct {
	N   int
	Min int64
	Max int64
	Avg float64
	P50 int64
	P95 int64
}

// Summarize copies vals, sorts, and fills Summary.
func Summarize(vals []int64) Summary {
	if len(vals) == 0 {
		return Summary{}
	}
	cp := append([]int64(nil), vals...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	var sum int64
	for _, v := range cp {
		sum += v
	}
	return Summary{
		N:   len(cp),
		Min: cp[0],
		Max: cp[len(cp)-1],
		Avg: float64(sum) / float64(len(cp)),
		P50: PercentileNearestRank(append([]int64(nil), cp...), 50),
		P95: PercentileNearestRank(append([]int64(nil), cp...), 95),
	}
}
