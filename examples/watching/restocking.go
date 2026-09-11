package watching

// A program worth watching.
//
// Small, and deliberately not tidy: it names its stages, holds a resource in a
// scope, retries a supplier that refuses twice, and fails on one item out of
// three. Every one of those is a boundary the runtime emits an event at, which
// is what makes the trace it produces worth reading rather than a single line
// saying "ran".

import (
	"context"
	"strings"

	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// Refusal is the program's own failure. The transport of telemetry has nothing
// to do with it, which is the point of the failure channel being the
// application's.
type Refusal struct {
	Item string
	Why  string
}

func (refusal Refusal) Error() string {
	return "restocking " + refusal.Item + ": " + refusal.Why
}

type restocking[A any] = effect.Effect[effect.Unit, Refusal, A]

// Restock reads a stock level for each item and writes it back.
//
// Direct style, because the sequence is dependent and there is no defer in the
// body -- the resource's lifetime is the scope's, which is what a scope is
// for.
func Restock(items ...string) restocking[[]int] {
	return effect.Scoped(func(scope effect.Scope) restocking[[]int] {
		return direct.Run(func(bind *direct.Binder[effect.Unit, Refusal]) []int {
			supplier := direct.Bind(bind, connected(scope))
			counted := make([]int, 0, len(items))
			for _, item := range items {
				counted = append(counted, direct.Bind(bind, restocked(supplier, item)))
			}
			return counted
		})
	}).WithSpan("restock")
}

// connected holds a supplier for the length of the scope, so the trace shows a
// resource acquired and released around everything between.
func connected(scope effect.Scope) restocking[*supplier] {
	operations := effect.For[effect.Unit, Refusal]()
	return scope.AcquireRelease(
		operations.Succeed(&supplier{attempts: map[string]int{}}).Named("connect"),
		func(*supplier) effect.Effect[effect.Unit, effect.Never, effect.Unit] {
			return effect.AddFinalizer[effect.Unit](func(context.Context) error { return nil })
		},
	)
}

// restocked is one item's own span, so the trace has a child per item and the
// aggregate has a measurement per item.
func restocked(from *supplier, item string) restocking[int] {
	return reading(from, item).RetryN(3).
		Named("read-level").
		WithSpan("item")
}

// reading refuses twice per item before answering, and refuses an item nobody
// stocks however often it is asked. A retry that eventually succeeds and one
// that exhausts are different events, and a trace should show both.
func reading(from *supplier, item string) restocking[int] {
	return effect.From(func(context.Context, effect.Unit) effect.Exit[Refusal, int] {
		if strings.HasPrefix(item, "unstocked") {
			return effect.ExitFailure[Refusal, int](
				Refusal{Item: item, Why: "nobody stocks it"})
		}
		if from.attempts[item] < 2 {
			from.attempts[item]++
			return effect.ExitFailure[Refusal, int](
				Refusal{Item: item, Why: "the supplier was busy"})
		}
		return effect.ExitSuccess[Refusal](len(item) * 10)
	})
}

// supplier is the held resource, and the state that makes the refusals
// deterministic: the same program run twice produces the same trace.
type supplier struct {
	attempts map[string]int
}
