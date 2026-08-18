package schema

import "testing"

// The expected values were produced by running shared/schema.ts. Buckets from
// either implementation land in the same cache and the same snapshot, so a
// disagreement here is a silently wrong histogram, not a style difference.
func TestHistogramBucketMatchesTypeScript(t *testing.T) {
	cases := []struct {
		size int64
		want int
	}{
		{0, 0},
		{1, 0},
		{2, 1},
		{3, 1},
		{4, 2},
		{7, 2},
		{8, 3},
		{1023, 9},
		{1024, 10},
		{1048576, 20},
		{104857600, 26},
		// The TypeScript original switches from clz32 to log2 at 4 GB. Both
		// sides of that seam have to agree.
		{0xffffffff, 31},
		{0x100000000, 32},
		{12884901888, 33},
		// Past 2^47 everything clamps to the last bucket.
		{1 << 52, 47},
		{1 << 53, 47},
	}

	for _, c := range cases {
		if got := HistogramBucket(c.size); got != c.want {
			t.Errorf("HistogramBucket(%d) = %d, want %d", c.size, got, c.want)
		}
	}
}

func TestHistogramQuantile(t *testing.T) {
	histogram := []int64{0, 0, 5, 0, 5}

	if got := HistogramQuantile(histogram, 0.5); got != 4 {
		t.Errorf("median = %d, want 4", got)
	}
	if got := HistogramQuantile(histogram, 0.95); got != 16 {
		t.Errorf("p95 = %d, want 16", got)
	}
	if got := HistogramQuantile(nil, 0.5); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
}

func TestEmptyHistogramHasEveryBucket(t *testing.T) {
	if len(EmptyHistogram()) != HistogramBuckets {
		t.Errorf("EmptyHistogram() has %d buckets, want %d", len(EmptyHistogram()), HistogramBuckets)
	}
}
