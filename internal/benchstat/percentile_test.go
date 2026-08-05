package benchstat

import "testing"

func TestPercentileNearestRank(t *testing.T) {
	t.Parallel()
	if PercentileNearestRank(nil, 50) != 0 {
		t.Fatal("empty")
	}
	if PercentileNearestRank([]int64{42}, 50) != 42 {
		t.Fatal("single")
	}
	// Odd count: 10,20,30,40,50 → p50 = 30
	got := PercentileNearestRank([]int64{50, 10, 30, 40, 20}, 50)
	if got != 30 {
		t.Fatalf("p50 = %d", got)
	}
	// Even: 1,2,3,4 → p50 nearest-rank index round(0.5*3)=2 → 3
	got = PercentileNearestRank([]int64{4, 1, 3, 2}, 50)
	if got != 3 {
		t.Fatalf("p50 even = %d", got)
	}
	if PercentileNearestRank([]int64{1, 2, 3, 4, 5}, 0) != 1 {
		t.Fatal("p0")
	}
	if PercentileNearestRank([]int64{1, 2, 3, 4, 5}, 100) != 5 {
		t.Fatal("p100")
	}
}

func TestSummarize(t *testing.T) {
	t.Parallel()
	s := Summarize([]int64{100, 200, 300, 400, 500})
	if s.N != 5 || s.Min != 100 || s.Max != 500 {
		t.Fatalf("%+v", s)
	}
	if s.P50 != 300 {
		t.Fatalf("p50 %d", s.P50)
	}
	if s.Avg != 300 {
		t.Fatalf("avg %v", s.Avg)
	}
	// Does not mutate input order requirement beyond copy.
	in := []int64{3, 1, 2}
	_ = Summarize(in)
	if in[0] != 3 {
		t.Fatalf("input mutated: %v", in)
	}
}
