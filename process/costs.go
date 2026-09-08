package process

// What the process spent while named work ran.
//
// The nearest honest thing to per-span memory and CPU. Go has no per-goroutine
// counter for either, so what can be measured is the process over a window,
// and what a window can be attributed to is the caller's judgement. The field
// names carry the caveat rather than a paragraph nobody reads: AllocatedDuring
// is what the process allocated *during* the work, which on a busy program
// includes everything else that ran.
//
// That is still worth having. A handler that allocates ten megabytes per
// request shows up here the first time it is looked at, and no amount of span
// nesting would have said so.

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// Unnamed is what work outside the declared vocabulary is accounted under.
const Unnamed = "other"

// Cost is what the process spent while one name's work ran, summed.
type Cost struct {
	Name string
	// Times is how often work under this name ran.
	Times uint64
	// AllocatedDuring is the bytes the process allocated while it ran, and
	// CPUSecondsDuring the CPU time the process used. Process-wide: concurrent
	// work is in these numbers too.
	AllocatedDuring  uint64
	CPUSecondsDuring float64
	// Elapsed is the wall-clock time the runs took together, and Longest the
	// slowest single run.
	Elapsed time.Duration
	Longest time.Duration
	// Collections is how many garbage collections completed during the runs,
	// which is what makes a large AllocatedDuring readable: allocation that
	// never provokes a collection costs nothing to collect.
	Collections uint64
}

// PerRun is the bytes allocated per run, which is the number worth comparing
// between two names.
func (cost Cost) PerRun() uint64 {
	if cost.Times == 0 {
		return 0
	}
	return cost.AllocatedDuring / cost.Times
}

// Costs accounts what named work spent, over a bounded set of names.
//
// Bounded for the reason a metric label is: a name per request is a series per
// request. An unlisted name is accounted under Unnamed.
type Costs struct {
	allowed map[string]bool

	mutex sync.Mutex
	held  map[string]Cost
}

// Accounting makes an account over the names worth distinguishing.
func Accounting(names ...string) *Costs {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		if name != "" && name != Unnamed {
			allowed[name] = true
		}
	}
	return &Costs{allowed: allowed, held: map[string]Cost{}}
}

// Record adds one run's change to a name's account.
func (costs *Costs) Record(name string, change Change) {
	under := Unnamed
	if costs.allowed[name] {
		under = name
	}

	costs.mutex.Lock()
	defer costs.mutex.Unlock()
	held := costs.held[under]
	held.Name = under
	held.Times++
	held.AllocatedDuring += change.AllocatedBytes
	held.CPUSecondsDuring += change.CPUSeconds
	held.Elapsed += change.Over
	held.Collections += change.GCCycles
	if change.Over > held.Longest {
		held.Longest = change.Over
	}
	costs.held[under] = held
}

// Snapshot is the accounts as they stand, the most allocated first.
//
// That order rather than by name, because the question this gets opened for is
// which work is spending the most.
func (costs *Costs) Snapshot() []Cost {
	costs.mutex.Lock()
	defer costs.mutex.Unlock()
	taken := make([]Cost, 0, len(costs.held))
	for _, held := range costs.held {
		taken = append(taken, held)
	}
	slices.SortFunc(taken, func(first Cost, second Cost) int {
		if by := cmp.Compare(second.AllocatedDuring, first.AllocatedDuring); by != 0 {
			return by
		}
		return cmp.Compare(first.Name, second.Name)
	})
	return taken
}

// Costing measures what the process spent while an effect ran and records it
// under a name.
//
// The reading is taken when the effect is interpreted and not when it is
// described, so one description measured twice records two runs. The second
// reading is a finalizer, so work that failed or was interrupted is accounted
// too -- a request that allocated a hundred megabytes and then gave up is
// exactly the one worth seeing.
func Costing[R, E, A any](
	costs *Costs,
	name string,
	fx effect.Effect[R, E, A],
) effect.Effect[R, E, A] {
	if costs == nil {
		return fx
	}
	operations := effect.For[R, E]()
	return operations.Suspend(func() effect.Effect[R, E, A] {
		before := Read()
		return fx.Ensuring(effect.Release[R](func(context.Context) error {
			costs.Record(name, Between(before, Read()))
			return nil
		}))
	})
}

// Measured is Costing with a name and a span: the three things wanted together
// whenever a stage of some work is worth accounting for separately.
//
//	direct.Bind(bind, process.Measured(costs, "score", scoring(notes)))
//
// The name is the span's, the account's key and the metric label all at once,
// so a stage appears in a trace, in the aggregate and in the account under one
// word -- and a caller has one place to change it.
func Measured[R, E, A any](
	costs *Costs,
	name string,
	fx effect.Effect[R, E, A],
) effect.Effect[R, E, A] {
	return Costing(costs, name, fx).Named(name).WithSpan(name)
}
