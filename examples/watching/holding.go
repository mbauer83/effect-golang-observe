package watching

// Work that is still running when somebody looks.
//
// A live view of a program that has finished is empty by construction: a fiber
// is forgotten when it completes and a span when it ends. So a demonstration
// of one has to be sampled while work is in flight, which is not a
// contrivance -- sampling a running program is the only time the question gets
// asked.

import (
	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

// Held is work in flight, and the release that lets it finish.
type Held struct {
	// Fibers are the forked fibers, every one of them running.
	Fibers []effect.Fiber[Refusal, int]
	// Release lets them all finish. Completing it twice is harmless.
	Release effect.Deferred[Refusal, effect.Unit]
}

// Holding forks one fiber per item, each waiting for the release, and returns
// only once every one of them is running.
//
// Forking is not the same as running: Fork hands back a handle and the fiber
// begins on a goroutine of its own, so a sample taken between the two sees
// some of the fibers and not others. Each fiber therefore says it has begun
// before it waits, and this waits for all of them -- which is what makes a
// live view something a program can assert on rather than observe by luck.
func Holding(count int) restocking[Held] {
	return direct.Run(func(do *direct.Do[effect.Unit, Refusal]) Held {
		operations := effect.For[effect.Unit, Refusal]()
		release := do.Await(operations.WidenError(effect.NewDeferred[effect.Unit, Refusal, effect.Unit]()))

		held := Held{Release: release, Fibers: make([]effect.Fiber[Refusal, int], 0, count)}
		begun := make([]effect.Deferred[Refusal, effect.Unit], 0, count)
		for index := range count {
			signal := do.Await(operations.WidenError(effect.NewDeferred[effect.Unit, Refusal, effect.Unit]()))
			begun = append(begun, signal)
			held.Fibers = append(held.Fibers,
				do.Await(operations.WidenError(
					effect.Fork[effect.Unit](waiting(signal, release, index)))))
		}
		for _, signal := range begun {
			do.Await(signal.Await[effect.Unit]())
		}
		return held
	}).WithSpan("hold")
}

// waiting is one held fiber: it says it has begun, then waits to be let go.
func waiting(
	signal effect.Deferred[Refusal, effect.Unit],
	release effect.Deferred[Refusal, effect.Unit],
	index int,
) restocking[int] {
	operations := effect.For[effect.Unit, Refusal]()
	return operations.WidenError(signal.Succeed[effect.Unit](effect.Unit{})).
		FlatMap(func(bool) restocking[int] {
			return release.Await[effect.Unit]().Map(func(effect.Unit) int { return index })
		}).
		WithName("await-release").
		WithSpan("holding")
}

// Finish releases the held work and waits for every fiber.
func Finish(held Held) restocking[[]int] {
	return direct.Run(func(do *direct.Do[effect.Unit, Refusal]) []int {
		operations := effect.For[effect.Unit, Refusal]()
		do.Await(operations.WidenError(held.Release.Succeed[effect.Unit](effect.Unit{})))
		finished := make([]int, 0, len(held.Fibers))
		for _, fiber := range held.Fibers {
			finished = append(finished, do.Await(fiber.Join[effect.Unit]()))
		}
		return finished
	}).WithSpan("finish")
}
