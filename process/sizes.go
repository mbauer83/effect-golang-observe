package process

// What sizes the allocations were.
//
// The distinction the byte count cannot make. A hundred kilobytes in one
// buffer and a hundred kilobytes in four thousand interface boxes are the same
// bytes and completely different problems: an allocation costs tens of
// nanoseconds and a pointer for the collector to chase whatever its size, so
// the many small ones are the expensive shape.
//
// Go reports the shape directly, as a histogram of allocation sizes
// cumulative since the process started -- so differencing two readings gives
// the sizes of the allocations in a window, which is the same trick every
// other counter here uses. Reading it costs about twenty nanoseconds more
// than reading the scalars alone, measured, which is why it is here at all.

import (
	"math"
	"runtime/metrics"
	"slices"
)

// sizeHistogram is the metric this reads. Its buckets are Go's own choice of
// size classes, so nothing here decides where the boundaries are.
const sizeHistogram = "/gc/heap/allocs-by-size:bytes"

// Sizes is a reading of the allocation-size histogram, cumulative since the
// process started.
//
// Bounds are the upper edge of each bucket and Counts how many allocations
// have fallen at or below it and above the one before. A Sizes on its own says
// what the process has ever done; the difference between two says what it did
// in between.
type Sizes struct {
	Bounds []float64
	Counts []uint64
}

// ReadSizes takes one reading of the histogram.
func ReadSizes() Sizes {
	samples := []metrics.Sample{{Name: sizeHistogram}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindFloat64Histogram {
		return Sizes{}
	}
	held := samples[0].Value.Float64Histogram()
	return Sizes{
		Bounds: slices.Clone(held.Buckets),
		Counts: slices.Clone(held.Counts),
	}
}

// Spread is the sizes of the allocations in one window: a bucket per size
// class, with the count of allocations that fell in it.
type Spread struct {
	Classes []Class
	// Total is how many allocations the window made, which is the sum of the
	// classes and the number a share is taken against.
	Total uint64
}

// Class is one size class and how many allocations fell in it.
type Class struct {
	// AtMost is the upper edge of the class, or +Inf for the last one.
	AtMost float64
	Count  uint64
}

// Share is the part of the window's allocations that fell in this class.
func (class Class) Share(total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(class.Count) / float64(total)
}

// Spreading is the sizes of the allocations between two readings.
//
// Only the classes that saw something are returned. Sixty-eight buckets of
// which four are non-zero is a table nobody can read, and the empty ones say
// nothing a reader did not already know.
func Spreading(before Sizes, after Sizes) Spread {
	if len(before.Counts) != len(after.Counts) || len(after.Counts) == 0 {
		return Spread{Classes: []Class{}}
	}
	spread := Spread{Classes: make([]Class, 0, 8)}
	for index := range after.Counts {
		count := since(before.Counts[index], after.Counts[index])
		if count == 0 {
			continue
		}
		spread.Total += count
		spread.Classes = append(spread.Classes,
			Class{AtMost: boundOf(after.Bounds, index), Count: count})
	}
	return spread
}

// boundOf is the upper edge of one bucket.
//
// A histogram's buckets are the intervals between its bounds, so bucket i ends
// at bounds[i+1]; the last has no upper bound and is reported as +Inf, which
// is what it is.
func boundOf(bounds []float64, index int) float64 {
	if index+1 < len(bounds) {
		return bounds[index+1]
	}
	return math.Inf(1)
}

// Largest is the class that holds the most allocations, and false when the
// window made none.
//
// The one number a reader wants first: whether this work allocates mostly
// small things or mostly large ones.
func (spread Spread) Largest() (Class, bool) {
	if len(spread.Classes) == 0 {
		return Class{}, false
	}
	largest := spread.Classes[0]
	for _, class := range spread.Classes[1:] {
		if class.Count > largest.Count {
			largest = class
		}
	}
	return largest, true
}

// Bands are the size classes gathered into a handful a person can read.
//
// Go's histogram has sixty-eight classes and a busy program touches nearly all
// of them, so the raw classes are a wall rather than a disclosure. These six
// answer the question the detail was opened for -- were these many tiny
// allocations or a few large ones -- and nothing is lost that a reader of six
// rows was going to notice in sixty-eight.
//
// The edges are where Go's own behaviour changes: the tiny allocator handles
// the first, the size classes the next three, and everything past 32KiB is a
// large object allocated straight from the heap.
var bands = []float64{64, 256, 1 << 10, 1 << 12, 1 << 15}

// Banded gathers the spread into the bands above, keeping only those that saw
// something.
func (spread Spread) Banded() Spread {
	if len(spread.Classes) == 0 {
		return Spread{Classes: []Class{}}
	}
	counts := make([]uint64, len(bands)+1)
	for _, class := range spread.Classes {
		counts[bandOf(class.AtMost)] += class.Count
	}

	banded := Spread{Classes: make([]Class, 0, len(counts)), Total: spread.Total}
	for index, count := range counts {
		if count == 0 {
			continue
		}
		edge := math.Inf(1)
		if index < len(bands) {
			edge = bands[index]
		}
		banded.Classes = append(banded.Classes, Class{AtMost: edge, Count: count})
	}
	return banded
}

// bandOf is the band a class's upper edge falls in.
func bandOf(atMost float64) int {
	for index, edge := range bands {
		if atMost <= edge {
			return index
		}
	}
	return len(bands)
}
