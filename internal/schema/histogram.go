package schema

import "math/bits"

// HistogramBucket returns the power-of-two bucket a size falls in: bucket i
// counts sizes in [2^i, 2^(i+1)). Bucket 0 also absorbs zero-byte files.
//
// The TypeScript original splits on 4 GB because clz32 only sees the low 32
// bits. Go has no such limit, so this is a single leading-zero count — but the
// results must agree exactly, since a warm cache mixes buckets written by
// either implementation.
func HistogramBucket(size int64) int {
	if size < 2 {
		return 0
	}
	bucket := bits.Len64(uint64(size)) - 1
	if bucket > HistogramBuckets-1 {
		return HistogramBuckets - 1
	}
	return bucket
}

// EmptyHistogram allocates a zeroed bucket array.
func EmptyHistogram() []int64 {
	return make([]int64, HistogramBuckets)
}

// HistogramQuantile approximates a quantile from a power-of-two histogram,
// reported at the bucket's lower edge. Accurate to a power of two, which is all
// the histogram ever knew.
func HistogramQuantile(histogram []int64, q float64) int64 {
	var total int64
	for _, count := range histogram {
		total += count
	}
	if total == 0 {
		return 0
	}

	target := float64(total) * q
	var seen int64
	for i, count := range histogram {
		seen += count
		if float64(seen) >= target {
			if i == 0 {
				return 0
			}
			return 1 << uint(i)
		}
	}
	return 1 << uint(len(histogram)-1)
}
