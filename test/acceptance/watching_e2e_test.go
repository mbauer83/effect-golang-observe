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

	"github.com/mbauer83/effect-golang-observe/examples/telemetry"
	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

// runRestock runs the example program under the example's own telemetry and
// closes the runtime, which is what drains the queue.
func runRestock(t *testing.T, items ...string) (*telemetry.Watch, effect.Exit[telemetry.Refusal, []int]) {
	t.Helper()
	watch, err := telemetry.NewWatch(256, "restock", "item", "read-level")
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

	exit := runtime.Run(context.Background(), effect.Unit{}, telemetry.Restock(items...))
	if live := runtime.LiveWork(); !live.IsEmpty() {
		t.Fatalf("the program left work behind: %#v", live)
	}
	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		t.Fatalf("closing reported %s", cleanup)
	}
	return watch, exit
}

func TestTheTraceHasTheShapeTheProgramRan(t *testing.T) {
	watch, exit := runRestock(t, "lamp", "pallet", "unstocked-widget")
	if !exit.IsFailure() {
		t.Fatalf("expected the unstocked item to refuse the program, got %v", exit)
	}

	tree := watch.Trace()
	if len(tree.Roots) != 1 || tree.Roots[0].Name != "restock" {
		t.Fatalf("expected one root span, got %v", spanNames(tree.Roots))
	}
	restock := tree.Roots[0]
	if len(restock.Children) != 3 {
		t.Fatalf("expected a child span per item, got %v", spanNames(restock.Children))
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
	watch, _ := runRestock(t, "lamp", "unstocked-widget")

	failed := watch.Trace().Unsuccessful()
	// The root and the one item: a failure that reached the root is still the
	// root's outcome, and reporting only the leaf would hide that.
	if len(failed) != 2 {
		t.Fatalf("expected the item and the root, got %v", spanNames(failed))
	}
	if failed[0].Name != "restock" || failed[1].Name != "item" {
		t.Fatalf("expected the root then the item, got %v", spanNames(failed))
	}
	if exhausted := retriesExhausted(watch.Trace()); exhausted != 1 {
		t.Fatalf("expected the one exhausted retry, got %d", exhausted)
	}
}

func TestNothingIsLeftOpenWhenTheProgramHasFinished(t *testing.T) {
	// The live tracker's whole claim: a span costs memory while it runs and
	// nothing afterwards.
	watch, _ := runRestock(t, "lamp", "pallet")

	if count := watch.Spans.Count(); count != 0 {
		t.Fatalf("expected nothing open, got %v", spanNames(watch.Spans.Open()))
	}
	if started, ended := watch.Spans.Starts(), watch.Spans.Ends(); started != ended {
		t.Fatalf("expected every span it saw start to have ended, got %d and %d", started, ended)
	}
	if open := watch.Trace().Open(); len(open) != 0 {
		t.Fatalf("expected no open span in the trace either, got %v", spanNames(open))
	}
}

func TestClosingTheRuntimeIsWhatDeliversAQueuedObserversEvents(t *testing.T) {
	// The Flusher wiring, end to end: the window is behind a queue, so what
	// it holds before the runtime closes is not what it holds after.
	watch, err := telemetry.NewWatch(256, "restock")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := effect.NewRuntime(effect.WithObserver(watch.Observer()))
	if err != nil {
		t.Fatal(err)
	}
	runtime.Run(context.Background(), effect.Unit{}, telemetry.Restock("lamp"))

	if cleanup := runtime.Close(context.Background()); !cleanup.IsEmpty() {
		t.Fatalf("closing reported %s", cleanup)
	}
	if seen := watch.Window.Count(); seen == 0 {
		t.Fatal("expected the queue drained into the window by Close")
	}
	if dropped := watch.Drops(); dropped != 0 {
		t.Fatalf("expected a window of this size to lose nothing, got %d", dropped)
	}
}

func TestMeasurementsCountTheWorkAndBoundTheirOwnLabels(t *testing.T) {
	watch, _ := runRestock(t, "lamp", "pallet", "unstocked-widget")
	snapshot := watch.Collector.Snapshot()

	// Three items, each its own span.
	succeeded := metrics.Label{
		Kind: effect.EventSpanEnded, Status: effect.EventStatusSuccess, Operation: "item",
	}
	if count := snapshot.Counts[succeeded]; count != 2 {
		t.Fatalf("expected two items to have succeeded, got %d", count)
	}
	if unsuccessful := snapshot.Unsuccessful(effect.EventSpanEnded); unsuccessful != 2 {
		t.Fatalf("expected the failing item and the root, got %d", unsuccessful)
	}
	// The runtime's own events name no operation, so they are measured under
	// Unnamed -- which is not Other: "this names no operation" and "this
	// names one nobody declared" are different facts, and a reader who cannot
	// tell them apart goes looking for work that does not exist.
	operations := map[string]bool{}
	for _, label := range snapshot.Labels() {
		operations[label.Operation] = true
	}
	if !operations[metrics.Anonymous] {
		t.Fatalf("expected the runtime's own events under Unnamed, got %v", snapshot.Labels())
	}
	// And the count of labels is bounded by the vocabulary either way: the
	// declared names, Other, and Unnamed.
	for operation := range operations {
		if operation != metrics.Anonymous && operation != metrics.Other &&
			!slices.Contains([]string{"restock", "item", "read-level"}, operation) {
			t.Fatalf("expected a declared name, Other or Unnamed, got %q", operation)
		}
	}
	if distribution := snapshot.Durations[succeeded]; distribution.Count != 2 || distribution.Max == 0 {
		t.Fatalf("expected both items measured, got %+v", distribution)
	}
}

func spanNames(spans []trace.Span) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}
	return names
}

func retriesExhausted(tree trace.Trace) int {
	count := 0
	for _, span := range tree.Spans() {
		for _, event := range span.Events {
			if event.Kind == effect.EventRetryExhausted {
				count++
			}
		}
	}
	return count
}
