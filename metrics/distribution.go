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

// Quantile is the tightest bound this distribution can put on where the given
// share of measurements fell -- an upper bound and not an interpolation,
// because a bucketed histogram knows bounds and does not know values.
//
// Never coarser than Max. A bucket bounded at a hundred microseconds holding a
// two-microsecond measurement is not wrong about the bound, but reporting it
// beside a longest of two microseconds reads as a contradiction -- and Max is
// the tighter bound, because every measurement is at or below it. So the
// answer is whichever of the two says more.
//
// A share outside (0, 1] is answered as the largest bound, which is the only
// bound that is true of every measurement.
func (distribution Distribution) Quantile(share float64) time.Duration {
	return distribution.atMost(distribution.bound(share))
}

// atMost is the tighter of a bucket bound and the largest measurement.
func (distribution Distribution) atMost(bound time.Duration) time.Duration {
	if distribution.Count > 0 && distribution.Max < bound {
		return distribution.Max
	}
	return bound
}

func (distribution Distribution) bound(share float64) time.Duration {
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
// none: a microsecond to ten seconds, by decades and halves.
//
// Chosen from what this actually measures, twice. Bounds starting at a
// millisecond put every in-process span in the first bucket; starting at a
// hundred microseconds still did, because a span around a Ref read or a
// handler that answers from memory takes single-digit microseconds. Seven
// decades with two bounds each tells those apart from the ones that waited on
// something, without pretending to more resolution than a bucketed histogram
// has.
var DefaultBounds = []time.Duration{
	time.Microsecond,
	5 * time.Microsecond,
	10 * time.Microsecond,
	50 * time.Microsecond,
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
