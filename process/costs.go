package process

// What the process spent while named work ran.
//
// The nearest honest thing to per-span memory and CPU. Go has no per-goroutine
// counter for either, so what can be measured is the process over a window,
// and what a window can be attributed to is the caller's judgement. The field
// names carry the caveat rather than a paragraph nobody reads: BytesDuring
// is what the process allocated *during* the work, which on a busy program
// includes everything else that ran.
//
// That is still worth having. A handler that allocates ten megabytes per
// request shows up here the first time it is looked at, and no amount of span
// nesting would have said so.

import (
	"cmp"
	"slices"
	"sync"
	"time"
)

// Other is what work outside the declared vocabulary is accounted under.
const Other = "other"

// Cost is what the process spent while one name's work ran, summed.
type Cost struct {
	Name string
	// Times is how often work under this name ran.
	Times uint64
	// BytesDuring is the bytes the process allocated while it ran,
	// ObjectsDuring how many allocations that was, and CPUSecondsDuring the
	// CPU time the process used. Process-wide: concurrent work is in these
	// numbers too.
	BytesDuring      uint64
	ObjectsDuring    uint64
	CPUSecondsDuring float64
	// Duration is the wall-clock time the runs took together, and Longest the
	// slowest single run.
	Duration time.Duration
	Longest  time.Duration
	// Collections is how many garbage collections completed during the runs,
	// which is what makes a large BytesDuring readable: allocation that
	// never provokes a collection costs nothing to collect.
	Collections uint64
	// Spread is the sizes of the allocations, or empty when the account was
	// not told to keep them. The disclosed layer: a reader wants the bytes
	// and the count first, and asks what shapes they were second.
	Spread Spread
	// Runs are the most recent runs of this name, newest first: the windows
	// the figures above are the average of. An average is what a name costs
	// and a run is what one span did, and both are wanted.
	Runs []Run
}

// PerRun is the bytes allocated per run, which is the number worth comparing
// between two names.
func (cost Cost) PerRun() uint64 {
	if cost.Times == 0 {
		return 0
	}
	return cost.BytesDuring / cost.Times
}

// ObjectsPerRun is how many allocations a run made, and MeanObjectBytes their
// average size.
//
// In Go the count is usually the more actionable of the two: an allocation is
// a few tens of nanoseconds and a pointer for the collector to chase whatever
// its size, so forty thousand small ones cost more than one large one holding
// the same bytes.
func (cost Cost) ObjectsPerRun() uint64 {
	if cost.Times == 0 {
		return 0
	}
	return cost.ObjectsDuring / cost.Times
}

func (cost Cost) MeanObjectBytes() uint64 {
	if cost.ObjectsDuring == 0 {
		return 0
	}
	return cost.BytesDuring / cost.ObjectsDuring
}

// Costs accounts what named work spent, over a bounded set of names.
//
// Bounded for the reason a metric label is: a name per request is a series per
// request. An unlisted name is accounted under Unnamed.
type Costs struct {
	names map[string]bool
	// keepsSizes says whether to keep the sizes of the allocations as well as
	// their count. Reading the histogram costs about twenty nanoseconds more
	// than the scalars, measured -- but keeping it is a bucket set per name,
	// and a caller that does not want the detail should not carry it.
	keepsSizes bool

	mutex  sync.Mutex
	totals map[string]Cost
	sizes  map[string]map[float64]uint64
	// runs are the recent windows behind each name's average, in runs.go.
	runs map[string]*runs
}

// NewCosts makes an account over the names worth distinguishing.
func NewCosts(names ...string) *Costs {
	return newCosts(false, names)
}

// NewCostsWithSizes is NewCosts that also keeps the sizes of the allocations, so a name
// can say whether it allocated a few large things or a great many small ones.
//
// A separate constructor because it carries more: a set of size classes per
// name, and a histogram read at each end of every window. The reading is
// nearly free -- twenty nanoseconds against the scalars -- and the keeping is
// what a caller is choosing here.
func NewCostsWithSizes(names ...string) *Costs {
	return newCosts(true, names)
}

func newCosts(sizing bool, names []string) *Costs {
	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		if name != "" && name != Other {
			nameSet[name] = true
		}
	}
	return &Costs{
		names:      nameSet,
		keepsSizes: sizing,
		totals:     map[string]Cost{},
		sizes:      map[string]map[float64]uint64{},
		runs:       map[string]*runs{},
	}
}

// KeepsSizes says whether this account keeps the sizes of the allocations.
func (costs *Costs) KeepsSizes() bool {
	return costs.keepsSizes
}

// RecordSpread adds one run's change together with the sizes its allocations
// fell into.
//
// Separate from Record because only a caller that sampled the histogram at
// both ends of the window has a spread to give, and one that did not should
// not have to pass an empty one.
func (costs *Costs) RecordSpread(name string, change Change, spread Spread) {
	costs.record(name, change, spread)
}

// Record adds one run's change to a name's account.
func (costs *Costs) Record(name string, change Change) {
	costs.record(name, change, Spread{})
}

func (costs *Costs) record(name string, change Change, spread Spread) {
	key := Other
	if costs.names[name] {
		key = name
	}

	costs.mutex.Lock()
	defer costs.mutex.Unlock()
	cost := costs.totals[key]
	cost.Name = key
	cost.Times++
	cost.BytesDuring += change.AllocatedBytes
	cost.ObjectsDuring += change.AllocatedObjects
	cost.CPUSecondsDuring += change.CPUSeconds
	cost.Duration += change.Duration
	cost.Collections += change.GCCycles
	if change.Duration > cost.Longest {
		cost.Longest = change.Duration
	}
	costs.totals[key] = cost

	ring, found := costs.runs[key]
	if !found {
		ring = &runs{}
		costs.runs[key] = ring
	}
	ring.add(Run{EndTime: change.EndTime, Change: change})

	if !costs.keepsSizes || spread.Total == 0 {
		return
	}
	// Summed by class across runs, keyed by the class's own upper edge --
	// Go's size classes, so nothing here decides where a boundary is.
	classes, known := costs.sizes[key]
	if !known {
		classes = map[float64]uint64{}
		costs.sizes[key] = classes
	}
	for _, class := range spread.Classes {
		classes[class.AtMost] += class.Count
	}
}

// Snapshot is the accounts as they stand, the most allocated first.
//
// That order rather than by name, because the question this gets opened for is
// which work is spending the most.
func (costs *Costs) Snapshot() []Cost {
	costs.mutex.Lock()
	defer costs.mutex.Unlock()
	snapshot := make([]Cost, 0, len(costs.totals))
	for name, cost := range costs.totals {
		cost.Spread = spreadOf(costs.sizes[name])
		if ring := costs.runs[name]; ring != nil {
			cost.Runs = ring.recent()
		}
		snapshot = append(snapshot, cost)
	}
	slices.SortFunc(snapshot, func(first Cost, second Cost) int {
		if by := cmp.Compare(second.BytesDuring, first.BytesDuring); by != 0 {
			return by
		}
		return cmp.Compare(first.Name, second.Name)
	})
	return snapshot
}

// spreadOf reads one name's size classes out, smallest first.
func spreadOf(classes map[float64]uint64) Spread {
	if len(classes) == 0 {
		return Spread{Classes: []Class{}}
	}
	spread := Spread{Classes: make([]Class, 0, len(classes))}
	for bound, count := range classes {
		spread.Total += count
		spread.Classes = append(spread.Classes, Class{AtMost: bound, Count: count})
	}
	slices.SortFunc(spread.Classes, func(first Class, second Class) int {
		return cmp.Compare(first.AtMost, second.AtMost)
	})
	return spread
}
