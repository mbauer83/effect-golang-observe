// Command observedemo runs the watched program and reports what watching it
// produced.
//
// It exists so the example is a program and not only a fixture, and because
// what this module produces is text a person reads: a trace nobody has looked
// at is a trace nobody knows is legible.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mbauer83/effect-golang-observe/examples/watching"
	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang/effect"
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
