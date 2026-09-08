package metrics

// How long something took, in a form that does not grow.

import (
	"math"
	"slices"
	"time"
)

// Bucket is how many measurements fell at or below a bound.
type Bucket struct {
	AtMost time.Duration
	Count  uint64
}

// Distribution is a set of durations summarised.
//
// Count, Sum, Min and Max are exact. The buckets are cumulative, as a
// histogram's are in every exposition format worth exporting to, so the last
// one holds every measurement and a quantile is read by walking them.
//
// Keeping the measurements themselves would be exact and unbounded, which is
// the one thing a long-lived aggregate may not be.
type Distribution struct {
	Count   uint64
	Sum     time.Duration
	Min     time.Duration
	Max     time.Duration
	Buckets []Bucket
}

// Mean is the average measurement, and zero when there are none.
func (distribution Distribution) Mean() time.Duration {
	if distribution.Count == 0 {
		return 0
	}
	return distribution.Sum / time.Duration(distribution.Count)
}

// Quantile is the bucket bound at or below which the given share of
// measurements fell -- an upper bound and not an interpolation, because a
// bucketed histogram knows the bound and does not know the value.
//
// A share outside (0, 1] is answered as the largest bound, which is the only
// bound that is true of every measurement.
func (distribution Distribution) Quantile(share float64) time.Duration {
	if distribution.Count == 0 || len(distribution.Buckets) == 0 {
		return 0
	}
	if share <= 0 || share > 1 {
		return distribution.Buckets[len(distribution.Buckets)-1].AtMost
	}
	// The rank is rounded up, not truncated: the median of three
	// measurements is the second, and uint64(0.5*3) is the first -- which
	// would report the thirty-third percentile under the median's name.
	wanted := uint64(math.Ceil(share * float64(distribution.Count)))
	if wanted == 0 {
		wanted = 1
	}
	for _, bucket := range distribution.Buckets {
		if bucket.Count >= wanted {
			return bucket.AtMost
		}
	}
	return distribution.Buckets[len(distribution.Buckets)-1].AtMost
}

// DefaultBounds are the bucket bounds a collector uses when the caller states
// none: a hundred microseconds to ten seconds, by decades and halves.
//
// Chosen for what this measures. Much of what a runtime brackets is in-process
// and takes microseconds -- a span around a pure computation, a scope holding
// one value -- so bounds starting at a millisecond would put every measurement
// in the first bucket and answer every quantile with the same number. Five
// decades with two bounds each tells "fast" from "slow" from "something is
// wrong" without pretending to more resolution than a bucketed histogram has.
var DefaultBounds = []time.Duration{
	100 * time.Microsecond,
	500 * time.Microsecond,
	time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	5 * time.Second,
	10 * time.Second,
}

// bounded is a distribution being accumulated.
type bounded struct {
	bounds  []time.Duration
	counts  []uint64
	count   uint64
	sum     time.Duration
	least   time.Duration
	largest time.Duration
}

func newBounded(bounds []time.Duration) *bounded {
	return &bounded{bounds: bounds, counts: make([]uint64, len(bounds))}
}

func (accumulating *bounded) add(measured time.Duration) {
	if accumulating.count == 0 || measured < accumulating.least {
		accumulating.least = measured
	}
	if measured > accumulating.largest {
		accumulating.largest = measured
	}
	accumulating.count++
	accumulating.sum += measured
	// Cumulative, so a measurement counts in its own bucket and every wider
	// one. A measurement past the last bound counts in none, and Count still
	// includes it -- which is how a reader tells "everything was fast" from
	// "the bounds are too narrow for this".
	for index, bound := range accumulating.bounds {
		if measured <= bound {
			accumulating.counts[index]++
		}
	}
}

func (accumulating *bounded) snapshot() Distribution {
	buckets := make([]Bucket, 0, len(accumulating.bounds))
	for index, bound := range accumulating.bounds {
		buckets = append(buckets, Bucket{AtMost: bound, Count: accumulating.counts[index]})
	}
	return Distribution{
		Count:   accumulating.count,
		Sum:     accumulating.sum,
		Min:     accumulating.least,
		Max:     accumulating.largest,
		Buckets: buckets,
	}
}

// sortedBounds keeps the bounds ascending and without repeats, so the buckets
// are cumulative in the order they are read.
func sortedBounds(bounds []time.Duration) []time.Duration {
	if len(bounds) == 0 {
		return slices.Clone(DefaultBounds)
	}
	sorted := slices.Clone(bounds)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}
