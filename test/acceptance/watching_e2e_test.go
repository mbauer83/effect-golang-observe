package acceptance

// The whole module against a real runtime.
//
// The unit suite states events and reads them, which is the right way to test
// the reading. What only this can answer is whether the events a runtime
// actually emits carry what the reading assumes: that a nested span names its
// parent, that a retry's events land inside the span they happened in, and
// that closing the runtime drains a queued observer.

import (
	"context"
	"slices"
	"testing"

	"github.com/mbauer83/effect-golang-observe/examples/watching"
	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

// watched runs the example program under the example's own telemetry and
// closes the runtime, which is what drains the queue.
func watched(t *testing.T, items ...string) (*watching.Watch, effect.Exit[watching.Refusal, []int]) {
	t.Helper()
	watch, err := watching.Watching(256, "restock", "item", "read-level")
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

	exit := runtime.Run(context.Background(), effect.Unit{}, watching.Restock(items...))
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("the program left work behind: %#v", live)
	}
	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		t.Fatalf("closing reported %s", cleanup)
	}
	return watch, exit
}

func TestTheTraceHasTheShapeTheProgramRan(t *testing.T) {
	watch, exit := watched(t, "lamp", "pallet", "unstocked-widget")
	if !exit.IsFailure() {
		t.Fatalf("expected the unstocked item to refuse the program, got %v", exit)
	}

	assembled := watch.Trace()
	if len(assembled.Roots) != 1 || assembled.Roots[0].Name != "restock" {
		t.Fatalf("expected one root span, got %v", named(assembled.Roots))
	}
	restock := assembled.Roots[0]
	if len(restock.Children) != 3 {
		t.Fatalf("expected a child span per item, got %v", named(restock.Children))
	}
	// The retries happened inside the item's span, which is the assumption the
	// whole fold rests on: a non-span event carries the span it happened in.
	first := restock.Children[0]
	if len(first.Events) != 3 {
		t.Fatalf("expected the retries inside the item span, got %v", first.Events)
	}
	if first.Events[0].Kind != effect.EventRetryScheduled ||
		first.Events[2].Kind != effect.EventRetrySucceeded {
		t.Fatalf("expected two scheduled and one succeeded, got %v", first.Events)
	}
	// The scope and the resource are the root's own, and they bracket the
	// items rather than preceding them.
	if len(restock.Events) != 5 {
		t.Fatalf("expected the scope and resource events on the root, got %v", restock.Events)
	}
	if restock.Events[0].Kind != effect.EventScopeOpened ||
		restock.Events[4].Kind != effect.EventScopeClosed {
		t.Fatalf("unexpected bracketing: %v", restock.Events)
	}
}

func TestTheFailingItemIsTheOneTheTraceNames(t *testing.T) {
	watch, _ := watched(t, "lamp", "unstocked-widget")

	failed := watch.Trace().Failed()
	// The root and the one item: a failure that reached the root is still the
	// root's outcome, and reporting only the leaf would hide that.
	if len(failed) != 2 {
		t.Fatalf("expected the item and the root, got %v", named(failed))
	}
	if failed[0].Name != "restock" || failed[1].Name != "item" {
		t.Fatalf("expected the root then the item, got %v", named(failed))
	}
	if exhausted := retriesExhausted(watch.Trace()); exhausted != 1 {
		t.Fatalf("expected the one exhausted retry, got %d", exhausted)
	}
}

func TestNothingIsLeftOpenWhenTheProgramHasFinished(t *testing.T) {
	// The live tracker's whole claim: a span costs memory while it runs and
	// nothing afterwards.
	watch, _ := watched(t, "lamp", "pallet")

	if count := watch.Running.Count(); count != 0 {
		t.Fatalf("expected nothing open, got %v", named(watch.Running.Open()))
	}
	if started, ended := watch.Running.Started(), watch.Running.Ended(); started != ended {
		t.Fatalf("expected every span it saw start to have ended, got %d and %d", started, ended)
	}
	if open := watch.Trace().Open(); len(open) != 0 {
		t.Fatalf("expected no open span in the trace either, got %v", named(open))
	}
}

func TestClosingTheRuntimeIsWhatDeliversAQueuedObserversEvents(t *testing.T) {
	// The Flusher wiring, end to end: the window is behind a queue, so what
	// it holds before the runtime closes is not what it holds after.
	watch, err := watching.Watching(256, "restock")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := effect.NewRuntime(effect.WithObserver(watch.Observer()))
	if err != nil {
		t.Fatal(err)
	}
	runtime.Run(context.Background(), effect.Unit{}, watching.Restock("lamp"))

	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		t.Fatalf("closing reported %s", cleanup)
	}
	if seen := watch.Window.Seen(); seen == 0 {
		t.Fatal("expected the queue drained into the window by Close")
	}
	if dropped := watch.Dropped(); dropped != 0 {
		t.Fatalf("expected a window of this size to lose nothing, got %d", dropped)
	}
}

func TestMeasurementsCountTheWorkAndBoundTheirOwnLabels(t *testing.T) {
	watch, _ := watched(t, "lamp", "pallet", "unstocked-widget")
	taken := watch.Collected.Snapshot()

	// Three items, each its own span.
	succeeded := metrics.Label{
		Kind: effect.EventSpanEnded, Status: effect.EventStatusSuccess, Operation: "item",
	}
	if counted := taken.Counts[succeeded]; counted != 2 {
		t.Fatalf("expected two items to have succeeded, got %d", counted)
	}
	if unsuccessful := taken.Unsuccessful(effect.EventSpanEnded); unsuccessful != 2 {
		t.Fatalf("expected the failing item and the root, got %d", unsuccessful)
	}
	// The runtime's own events name no operation, so they are measured under
	// Unnamed -- which is not Other: "this names no operation" and "this
	// names one nobody declared" are different facts, and a reader who cannot
	// tell them apart goes looking for work that does not exist.
	held := map[string]bool{}
	for _, label := range taken.Labels() {
		held[label.Operation] = true
	}
	if !held[metrics.Unnamed] {
		t.Fatalf("expected the runtime's own events under Unnamed, got %v", taken.Labels())
	}
	// And the count of labels is bounded by the vocabulary either way: the
	// declared names, Other, and Unnamed.
	for operation := range held {
		if operation != metrics.Unnamed && operation != metrics.Other &&
			!slices.Contains([]string{"restock", "item", "read-level"}, operation) {
			t.Fatalf("expected a declared name, Other or Unnamed, got %q", operation)
		}
	}
	if held := taken.Durations[succeeded]; held.Count != 2 || held.Max == 0 {
		t.Fatalf("expected both items measured, got %+v", held)
	}
}

func named(spans []trace.Span) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}
	return names
}

func retriesExhausted(assembled trace.Trace) int {
	counted := 0
	for _, span := range assembled.Spans() {
		for _, event := range span.Events {
			if event.Kind == effect.EventRetryExhausted {
				counted++
			}
		}
	}
	return counted
}
