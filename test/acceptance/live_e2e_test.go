package acceptance

// What is running, sampled while it is.
//
// Its own file because it is a different question from the other suite's. That
// one folds a finished run into a tree; this one looks at a program that has
// not finished -- which is the only time a live view has an answer, since a
// fiber is forgotten when it completes and a span when it ends.

import (
	"context"
	"testing"

	"github.com/mbauer83/effect-golang-observe/examples/watching"
	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

func TestWhatIsRunningIsAnsweredWhileItIsRunning(t *testing.T) {
	// The live view's whole purpose, and the only time it has an answer: a
	// fiber is forgotten when it completes and a span when it ends, so this
	// samples from inside one interpretation, between forking the work and
	// letting it go.
	watch, err := watching.NewWatch(256, "hold", "holding")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := effect.NewRuntime(
		effect.WithObserver(watch.Observer()),
		effect.WithDebugTracking(),
	)
	if err != nil {
		t.Fatal(err)
	}

	var snapshot struct {
		fibers []trace.Fiber
		spans  []trace.Span
		work   effect.LiveWork
	}
	program := effect.Gen(func(do *effect.Do[effect.Unit, watching.Refusal]) []int {
		gate := do.Await(watching.Hold(3))
		do.Await(sample(func() {
			snapshot.fibers = watch.Fibers.Tree()
			snapshot.spans = watch.Spans.Open()
			snapshot.work = runtime.LiveWork()
		}))
		return do.Await(watching.Finish(gate))
	})

	finished, done := runtime.Run(context.Background(), effect.Unit{}, program).Value()
	if !done || len(finished) != 3 {
		t.Fatalf("expected the three held fibers to finish, got %v", finished)
	}

	// Every fiber was running when it was sampled, because Holding waits for
	// each to say so: forking is not running, and a sample between the two
	// would see some of them.
	if len(snapshot.fibers) != 3 {
		t.Fatalf("expected three fibers running, got %d", len(snapshot.fibers))
	}
	// Oldest first, by the runtime's own timestamps.
	for index := 1; index < len(snapshot.fibers); index++ {
		if snapshot.fibers[index].StartTime.Before(snapshot.fibers[index-1].StartTime) {
			t.Fatalf("expected the oldest fiber first, got %v", snapshot.fibers)
		}
	}
	if len(snapshot.spans) != 3 {
		t.Fatalf("expected a span open per held fiber, got %v", spanNames(snapshot.spans))
	}
	// The runtime's own count agrees with what the tracker saw, which is the
	// cross-check worth having: two independent accounts of the same fact.
	if snapshot.work.Fibers != 3 {
		t.Fatalf("expected the runtime to own three fibers, got %#v", snapshot.work)
	}

	// And afterwards nothing is held, which is what makes the tracker safe to
	// install for the life of a program.
	if count := watch.Fibers.Count(); count != 0 {
		t.Fatalf("expected every fiber forgotten, got %d", count)
	}
	if started, ended := watch.Fibers.Starts(), watch.Fibers.Ends(); started != ended {
		t.Fatalf("expected every fiber that started to have ended, got %d and %d", started, ended)
	}
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("the program left work behind: %#v", live)
	}
}

// sample performs one side effect between two stages, which is what sampling
// a running program is.
func sample(look func()) effect.Effect[effect.Unit, watching.Refusal, effect.Unit] {
	return effect.From(func(context.Context, effect.Unit) effect.Exit[watching.Refusal, effect.Unit] {
		look()
		return effect.ExitSuccess[watching.Refusal](effect.Unit{})
	}).WithName("look")
}
