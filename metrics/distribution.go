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
	rank := uint64(math.Ceil(share * float64(distribution.Count)))
	if rank == 0 {
		rank = 1
	}
	for _, bucket := range distribution.Buckets {
		if bucket.Count >= rank {
			return bucket.AtMost
		}
	}
	return distribution.Buckets[len(distribution.Buckets)-1].AtMost
}

// DefaultBoundaries are the bucket bounds a collector uses when the caller states
// none: a microsecond to a minute, three to a decade.
//
// Chosen from what this actually measures, three times. Bounds starting at a
// millisecond put every in-process span in the first bucket; starting at a
// hundred microseconds still did, because a span around a Ref read or a
// handler that answers from memory takes single-digit microseconds.
//
// Then one and five to a decade proved too coarse for the thing people read
// these for: a request taking 1.1 seconds is reported at five, and a page
// whose median is six seconds reads as ten. A bound is honest -- every
// measurement did fall at or below it -- and an honest answer five times the
// truth is not worth much. One, two and five bound the error at two and a
// half times instead, for nine more buckets per label.
//
// It ends at a minute rather than ten seconds because work that waits on
// somebody else's service does take a minute, and a quantile pinned to the
// last bound says only "at least this". Beyond it Max is the answer, which is
// exact and is what every bound above the largest measurement collapses to.
var DefaultBoundaries = []time.Duration{
	time.Microsecond,
	2 * time.Microsecond,
	5 * time.Microsecond,
	10 * time.Microsecond,
	20 * time.Microsecond,
	50 * time.Microsecond,
	100 * time.Microsecond,
	200 * time.Microsecond,
	500 * time.Microsecond,
	time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	30 * time.Second,
	time.Minute,
}

// histogram is a distribution being accumulated.
type histogram struct {
	boundaries []time.Duration
	counts     []uint64
	count      uint64
	sum        time.Duration
	least      time.Duration
	largest    time.Duration
}

func newHistogram(bounds []time.Duration) *histogram {
	return &histogram{boundaries: bounds, counts: make([]uint64, len(bounds))}
}

func (accumulator *histogram) add(duration time.Duration) {
	if accumulator.count == 0 || duration < accumulator.least {
		accumulator.least = duration
	}
	if duration > accumulator.largest {
		accumulator.largest = duration
	}
	accumulator.count++
	accumulator.sum += duration
	// Cumulative, so a measurement counts in its own bucket and every wider
	// one. A measurement past the last bound counts in none, and Count still
	// includes it -- which is how a reader tells "everything was fast" from
	// "the bounds are too narrow for this".
	for index, bound := range accumulator.boundaries {
		if duration <= bound {
			accumulator.counts[index]++
		}
	}
}

func (accumulator *histogram) snapshot() Distribution {
	buckets := make([]Bucket, 0, len(accumulator.boundaries))
	for index, bound := range accumulator.boundaries {
		buckets = append(buckets, Bucket{AtMost: bound, Count: accumulator.counts[index]})
	}
	return Distribution{
		Count:   accumulator.count,
		Sum:     accumulator.sum,
		Min:     accumulator.least,
		Max:     accumulator.largest,
		Buckets: buckets,
	}
}

// sortBoundaries keeps the bounds ascending and without repeats, so the buckets
// are cumulative in the order they are read.
func sortBoundaries(bounds []time.Duration) []time.Duration {
	if len(bounds) == 0 {
		return slices.Clone(DefaultBoundaries)
	}
	boundaries := slices.Clone(bounds)
	slices.Sort(boundaries)
	return slices.Compact(boundaries)
}
