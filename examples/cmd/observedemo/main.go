// Command observedemo runs the watched program and reports what watching it
// produced.
//
// It exists so the example is a program and not only a fixture, and because
// what this module produces is text a person reads: a trace nobody has looked
// at is a trace nobody knows is legible.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mbauer83/effect-golang-observe/examples/watching"
	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang/effect"
	"github.com/mbauer83/effect-golang/experimental/direct"
)

func main() {
	// The operations worth their own measurements, named up front: that is
	// what keeps the number of series fixed before the program runs.
	watch, err := watching.Watching(256, "restock", "item", "read-level")
	if err != nil {
		fail(err)
	}
	runtime, err := effect.NewRuntime(
		effect.WithObserver(watch.Observer()),
		effect.WithDebugTracking(),
	)
	if err != nil {
		fail(err)
	}

	exit := runtime.Run(context.Background(), effect.Unit{},
		watching.Restock("lamp", "pallet", "unstocked-widget"))
	fmt.Printf("restocking: %s\n", outcome(exit))

	// Sampled while work is in flight, because that is the only time the
	// question has an answer: a fiber is forgotten when it completes and a
	// span when it ends, so a live view of a finished program is empty.
	reportLive(runtime, watch)

	// Closing drains the queue. Reading the window before this would show only
	// what had been delivered so far, which is the price of not paying for
	// delivery on the observed fiber.
	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		fmt.Printf("shutdown: %s\n", cleanup)
	}

	reportTrace(watch)
	reportMeasurements(watch)
	fmt.Printf("\nstill open: %d span(s); events dropped: %d\n",
		watch.Running.Count(), watch.Dropped())
}

// reportLive forks work that waits, looks at what is running, and lets it go.
//
// One interpretation, because a forked fiber belongs to the scope that forked
// it: sampling from a second Run would find the work already interrupted,
// which is the first thing this got wrong.
func reportLive(runtime *effect.Runtime, watch *watching.Watch) {
	program := direct.Run(func(bind *direct.Binder[effect.Unit, watching.Refusal]) []int {
		held := direct.Bind(bind, watching.Holding(3))
		direct.Bind(bind, looking(func() { showLive(watch) }))
		return direct.Bind(bind, watching.Finish(held))
	})
	if _, done := runtime.Run(context.Background(), effect.Unit{}, program).Value(); !done {
		fail(errors.New("the held work did not finish"))
	}
	fmt.Printf("  released: %d fiber(s) and %d span(s) still open\n",
		watch.Fibers.Count(), watch.Running.Count())
}

// showLive is the sampling itself: what is running, at the instant it is asked.
func showLive(watch *watching.Watch) {
	now := time.Now()
	fmt.Printf("\nwhat is running, sampled while it is (%d fiber(s), %d span(s) open)\n",
		watch.Fibers.Count(), watch.Running.Count())
	fmt.Print(indented(watch.Fibers.Render(now)))
	for _, span := range watch.Running.Open() {
		fmt.Printf("  %s open %s\n", span.Name, span.Age(now))
	}
}

// looking performs one side effect between two stages, which is what sampling
// a running program is.
func looking(look func()) effect.Effect[effect.Unit, watching.Refusal, effect.Unit] {
	return effect.From(func(context.Context, effect.Unit) effect.Exit[watching.Refusal, effect.Unit] {
		look()
		return effect.ExitSuccess[watching.Refusal](effect.Unit{})
	}).Named("look")
}

func reportTrace(watch *watching.Watch) {
	assembled := watch.Trace()
	fmt.Printf("\nwhat it did (%d span(s), %d event(s) outside every span)\n",
		len(assembled.Spans()), len(assembled.Loose))
	fmt.Print(indented(assembled.Render()))
	if failed := assembled.Failed(); len(failed) > 0 {
		fmt.Println("\nwhat failed")
		for _, span := range failed {
			fmt.Printf("  %s %s after %s\n", span.Name, span.Status, span.Duration)
		}
	}
}

func reportMeasurements(watch *watching.Watch) {
	taken := watch.Collected.Snapshot()
	fmt.Println("\nhow much of it there was")
	for _, label := range taken.Labels() {
		measured := ""
		if held, timed := taken.Durations[label]; timed {
			measured = fmt.Sprintf("  median at most %s, longest %s",
				held.Quantile(0.5), held.Max)
		}
		if held, delayed := taken.Delays[label]; delayed {
			measured += fmt.Sprintf("  waited %s in total", held.Sum)
		}
		fmt.Printf("  %-18s %-14s %-12s x%d%s\n",
			label.Kind, named(label.Operation), status(label.Status),
			taken.Counts[label], measured)
	}
}

func named(operation string) string {
	if operation == metrics.Other {
		return "(other)"
	}
	return operation
}

func status(held effect.EventStatus) string {
	if held == effect.EventStatusNone {
		return "-"
	}
	return string(held)
}

func outcome[E, A any](exit effect.Exit[E, A]) string {
	if value, ok := exit.Value(); ok {
		return fmt.Sprintf("succeeded with %v", value)
	}
	cause, _ := exit.Cause()
	return "refused: " + cause.String()
}

func indented(rendered string) string {
	out := ""
	for line := range splitLines(rendered) {
		out += "  " + line + "\n"
	}
	return out
}

// splitLines yields each non-empty line, so the indent is not applied to the
// blank one a trailing newline leaves behind.
func splitLines(rendered string) func(func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for index := range len(rendered) {
			if rendered[index] != '\n' {
				continue
			}
			if index > start && !yield(rendered[start:index]) {
				return
			}
			start = index + 1
		}
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "observedemo: %v\n", err)
	os.Exit(1)
}
