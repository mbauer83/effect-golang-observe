package process

// Measuring what a piece of work spent.
//
// The account is in costs.go; these are the two combinators that fill it. A
// file of their own because they are the only part of this package that knows
// about effects at all: everything else reads counters.

import (
	"context"

	"github.com/mbauer83/effect-golang/effect"
)

// Track measures what the process spent while an effect ran and records it
// under a name.
//
// The reading is taken when the effect is interpreted and not when it is
// described, so one description measured twice records two runs. The second
// reading is a finalizer, so work that failed or was interrupted is accounted
// too -- a request that allocated a hundred megabytes and then gave up is
// exactly the one worth seeing.
func Track[R, E, A any](
	costs *Costs,
	name string,
	fx effect.Effect[R, E, A],
) effect.Effect[R, E, A] {
	if costs == nil {
		return fx
	}
	operations := effect.For[R, E]()
	if !costs.KeepsSizes() {
		return operations.Suspend(func() effect.Effect[R, E, A] {
			before := Read()
			return fx.Ensuring(effect.AddFinalizer[R](func(context.Context) error {
				costs.Record(name, Diff(before, Read()))
				return nil
			}))
		})
	}
	// The account keeps sizes, so the histogram is sampled at both ends too.
	// Two paths rather than one that always samples it: an account that does
	// not keep the detail should not pay to gather it.
	return operations.Suspend(func() effect.Effect[R, E, A] {
		before, sizesBefore := Read(), ReadSizes()
		return fx.Ensuring(effect.AddFinalizer[R](func(context.Context) error {
			costs.RecordSpread(name, Diff(before, Read()),
				DiffSizes(sizesBefore, ReadSizes()))
			return nil
		}))
	})
}

// Measure is Track with a name and a span: the three things wanted together
// whenever a stage of some work is worth accounting for separately.
//
//	do.Await(process.Measure(costs, "score", scoring(notes)))
//
// The name is the span's, the account's key and the metric label all at once,
// so a stage appears in a trace, in the aggregate and in the account under one
// word -- and a caller has one place to change it.
func Measure[R, E, A any](
	costs *Costs,
	name string,
	fx effect.Effect[R, E, A],
) effect.Effect[R, E, A] {
	return Track(costs, name, fx).WithName(name).WithSpan(name)
}
