package process

// Where a name's runs fell, as opposed to what they cost on average.
//
// An average answers "which work is expensive" and hides the answer to "how
// bad does it get". A name that allocates a hundred kilobytes on almost every
// run and forty megabytes on one in fifty has an average that describes
// neither, and it is the one in fifty that wakes somebody up.
//
// Exact over the runs the account kept, which is a window and not the whole
// history: these are the real measurements sorted, not a histogram's bounds,
// so a quantile here is a measurement that happened rather than a bound it
// fell under. What the window costs is that a name running twice a second is
// described by its last two minutes -- which is the right window for a tool
// somebody is looking at, and the wrong one for a monthly report.

import (
	"slices"
	"time"
)

// AllocatedAt is the bytes a run of this name allocated, at the given share
// of its kept runs.
//
//	cost.AllocatedAt(0.5)   // what a typical run allocates
//	cost.AllocatedAt(0.99)  // what the worst one in a hundred allocates
func (cost Cost) AllocatedAt(share float64) uint64 {
	return atShare(cost.Runs, share, func(run Run) uint64 {
		return run.Change.AllocatedBytes
	})
}

// ObjectsAt is how many allocations a run made, at the given share.
//
// Worth reading beside the bytes rather than instead of them: in Go an
// allocation costs a few tens of nanoseconds and a pointer for the collector
// to chase whatever its size, so a run making forty thousand small ones is
// slower than one making a few large ones of the same weight.
func (cost Cost) ObjectsAt(share float64) uint64 {
	return atShare(cost.Runs, share, func(run Run) uint64 {
		return run.Change.AllocatedObjects
	})
}

// TookAt is how long a run took, at the given share.
func (cost Cost) TookAt(share float64) time.Duration {
	return time.Duration(atShare(cost.Runs, share, func(run Run) uint64 {
		if run.Change.Over < 0 {
			return 0
		}
		return uint64(run.Change.Over)
	}))
}

// KeptRunCount is how many runs these figures are drawn from, so a reader can
// tell a quantile over two hundred runs from one over three.
func (cost Cost) KeptRunCount() int { return len(cost.Runs) }

// atShare is the value at a share of the runs, read from the sorted
// measurements.
//
// The nearest-rank method: the value at position ceil(share x n), which is a
// measurement that happened rather than an interpolation between two that
// did. A share outside (0, 1] answers with the largest, which is the only
// value true of every run.
func atShare(runs []Run, share float64, of func(Run) uint64) uint64 {
	if len(runs) == 0 {
		return 0
	}
	values := make([]uint64, 0, len(runs))
	for _, run := range runs {
		values = append(values, of(run))
	}
	slices.Sort(values)
	return values[rankOf(share, len(values))]
}

// rankOf is the index of the value at a share of this many sorted values.
func rankOf(share float64, count int) int {
	if share <= 0 || share > 1 {
		return count - 1
	}
	rank := int(float64(count)*share + 0.9999999)
	if rank < 1 {
		rank = 1
	}
	if rank > count {
		rank = count
	}
	return rank - 1
}
