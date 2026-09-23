package telemetry

// Work that is still running when somebody looks.
//
// A live view of a program that has finished is empty by construction: a fiber
// is forgotten when it completes and a span when it ends. So a demonstration
// of one has to be sampled while work is in flight, which is not a
// contrivance -- sampling a running program is the only time the question gets
// asked.

import (
	"github.com/mbauer83/effect-golang/effect"
)

// Gate is work in flight, and the release that lets it finish.
type Gate struct {
	// Fibers are the forked fibers, every one of them running.
	Fibers []effect.Fiber[Refusal, int]
	// Release lets them all finish. Completing it twice is harmless.
	Release effect.Deferred[Refusal, effect.Unit]
}

// Hold forks one fiber per item, each waiting for the release, and returns
// only once every one of them is running.
//
// Forking is not the same as running: Fork hands back a handle and the fiber
// begins on a goroutine of its own, so a sample taken between the two sees
// some of the fibers and not others. Each fiber therefore says it has begun
// before it waits, and this waits for all of them -- which is what makes a
// live view something a program can assert on rather than observe by luck.
func Hold(count int) program[Gate] {
	return effect.Gen(func(do *effect.Do[effect.Unit, Refusal]) Gate {
		operations := effect.For[effect.Unit, Refusal]()
		release := do.Await(operations.WidenError(effect.NewDeferred[effect.Unit, Refusal, effect.Unit]()))

		gate := Gate{Release: release, Fibers: make([]effect.Fiber[Refusal, int], 0, count)}
		signals := make([]effect.Deferred[Refusal, effect.Unit], 0, count)
		for index := range count {
			signal := do.Await(operations.WidenError(effect.NewDeferred[effect.Unit, Refusal, effect.Unit]()))
			signals = append(signals, signal)
			gate.Fibers = append(gate.Fibers,
				do.Await(operations.WidenError(
					effect.Fork[effect.Unit](awaitRelease(signal, release, index)))))
		}
		for _, signal := range signals {
			do.Await(signal.Await[effect.Unit]())
		}
		return gate
	}).WithSpan("hold")
}

// awaitRelease is one held fiber: it says it has begun, then waits to be let go.
func awaitRelease(
	signal effect.Deferred[Refusal, effect.Unit],
	release effect.Deferred[Refusal, effect.Unit],
	index int,
) program[int] {
	operations := effect.For[effect.Unit, Refusal]()
	return effect.Gen(func(do *effect.Do[effect.Unit, Refusal]) int {
		do.Await(operations.WidenError(signal.Succeed[effect.Unit](effect.Unit{})))
		do.Await(release.Await[effect.Unit]())
		return index
	}).
		WithName("await-release").
		WithSpan("holding")
}

// Finish releases the held work and waits for every fiber.
func Finish(gate Gate) program[[]int] {
	return effect.Gen(func(do *effect.Do[effect.Unit, Refusal]) []int {
		operations := effect.For[effect.Unit, Refusal]()
		do.Await(operations.WidenError(gate.Release.Succeed[effect.Unit](effect.Unit{})))
		results := make([]int, 0, len(gate.Fibers))
		for _, fiber := range gate.Fibers {
			results = append(results, do.Await(fiber.Join[effect.Unit]()))
		}
		return results
	}).WithSpan("finish")
}
