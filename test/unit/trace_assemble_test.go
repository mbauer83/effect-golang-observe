package unit

// Reading a finite collection of events as a tree, including the collections
// that are not tidy: a span still open, an end whose start was truncated away,
// a child whose parent is outside the window.

import (
	"log/slog"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

func TestAssembleNestsSpansUnderTheOnesThatEnclosedThem(t *testing.T) {
	assembled := trace.Assemble([]effect.RuntimeEvent{
		spanStarted(1, 0, "outer", 0),
		spanStarted(2, 1, "inner", time.Millisecond),
		within(2, effect.EventRetryScheduled, "inner"),
		spanEnded(2, 1, "inner", 2*time.Millisecond, time.Millisecond, effect.EventStatusSuccess),
		spanStarted(3, 1, "sibling", 3*time.Millisecond),
		spanEnded(3, 1, "sibling", 4*time.Millisecond, time.Millisecond, effect.EventStatusFailure),
		spanEnded(1, 0, "outer", 5*time.Millisecond, 5*time.Millisecond, effect.EventStatusSuccess),
	})

	if len(assembled.Roots) != 1 || assembled.Roots[0].Name != "outer" {
		t.Fatalf("expected one root, got %v", assembled.Roots)
	}
	outer := assembled.Roots[0]
	if len(outer.Children) != 2 {
		t.Fatalf("expected two children, got %d", len(outer.Children))
	}
	// Oldest first, which is the order the work happened in and the only order
	// a reader can follow.
	if outer.Children[0].Name != "inner" || outer.Children[1].Name != "sibling" {
		t.Fatalf("expected them in the order they started, got %v", outer.Children)
	}
	// The events inside a span belong to that span and not to its parent.
	if len(outer.Events) != 0 {
		t.Fatalf("expected nothing directly inside the outer span, got %v", outer.Events)
	}
	if len(outer.Children[0].Events) != 1 ||
		outer.Children[0].Events[0].Kind != effect.EventRetryScheduled {
		t.Fatalf("expected the retry inside the inner span, got %v", outer.Children[0].Events)
	}
	// The duration is the runtime's measurement, not a subtraction of two
	// wall-clock readings.
	if outer.Duration != 5*time.Millisecond {
		t.Fatalf("expected the measured duration, got %v", outer.Duration)
	}
	if outer.Descendants() != 2 {
		t.Fatalf("expected two descendants, got %d", outer.Descendants())
	}
}

func TestASpanThatNeverEndedStaysOpen(t *testing.T) {
	// The report, not a gap: a span still open when the events ran out is
	// where a program that stopped responding is, and dropping it for being
	// incomplete would hide exactly that.
	assembled := trace.Assemble([]effect.RuntimeEvent{
		spanStarted(1, 0, "waiting", 0),
		spanStarted(2, 1, "finished", time.Millisecond),
		spanEnded(2, 1, "finished", 2*time.Millisecond, time.Millisecond, effect.EventStatusSuccess),
	})

	open := assembled.Open()
	if len(open) != 1 || open[0].Name != "waiting" {
		t.Fatalf("expected the unfinished span reported open, got %v", open)
	}
	if open[0].Duration != 0 || !open[0].Ended.IsZero() {
		t.Fatalf("expected no measurement for a span that has not ended, got %+v", open[0])
	}
	// An open span has not ended in anything, so it has not failed either.
	if open[0].Failed() {
		t.Error("expected an open span not to count as failed")
	}
}

func TestAnEndWithoutAStartIsLooseRatherThanAHalfSpan(t *testing.T) {
	// What a window that begins mid-run looks like. Inventing a span for the
	// end would put one in the tree that nothing observed.
	assembled := trace.Assemble([]effect.RuntimeEvent{
		within(9, effect.EventResourceReleased, "gone"),
		spanEnded(9, 0, "gone", time.Millisecond, time.Millisecond, effect.EventStatusSuccess),
	})

	if len(assembled.Roots) != 0 {
		t.Fatalf("expected no spans, got %v", assembled.Roots)
	}
	if len(assembled.Loose) != 2 {
		t.Fatalf("expected both events loose, got %v", assembled.Loose)
	}
}

func TestASpanWhoseParentIsOutsideTheWindowIsARootHere(t *testing.T) {
	assembled := trace.Assemble([]effect.RuntimeEvent{
		spanStarted(2, 1, "orphaned", 0),
		spanEnded(2, 1, "orphaned", time.Millisecond, time.Millisecond, effect.EventStatusSuccess),
	})

	if len(assembled.Roots) != 1 || assembled.Roots[0].Name != "orphaned" {
		t.Fatalf("expected the span rooted here, got %v", assembled.Roots)
	}
	// The parent it names is still recorded, because it is true and a reader
	// stitching two windows together needs it.
	if assembled.Roots[0].ParentID != 1 {
		t.Fatalf("expected the parent it named kept, got %d", assembled.Roots[0].ParentID)
	}
}

func TestTheEndsAttributesJoinTheStartsWithoutRepeatingThem(t *testing.T) {
	// The runtime supplies the metadata in force at each boundary, so the same
	// inherited attribute arrives twice and something the work found out
	// arrives only at the end.
	component := slog.String("component", "catalog")
	assembled := trace.Assemble([]effect.RuntimeEvent{
		attributed(spanStarted(1, 0, "load", 0), component),
		attributed(spanEnded(1, 0, "load", time.Millisecond, time.Millisecond,
			effect.EventStatusSuccess), component, slog.Int("found", 3)),
	})

	held := assembled.Roots[0].Attributes
	if len(held) != 2 {
		t.Fatalf("expected the repeat collapsed and the new one kept, got %v", held)
	}
	if held[0].Key != "component" || held[1].Key != "found" {
		t.Fatalf("expected the start's first, got %v", held)
	}
}

func TestRenderingIsStableAndSaysWhatEachSpanDid(t *testing.T) {
	assembled := trace.Assemble([]effect.RuntimeEvent{
		spanStarted(1, 0, "outer", 0),
		spanStarted(2, 1, "inner", time.Millisecond),
		within(2, effect.EventRetryScheduled, "inner"),
		spanEnded(2, 1, "inner", 2*time.Millisecond, 2*time.Millisecond, effect.EventStatusFailure),
		spanStarted(3, 1, "unfinished", 3*time.Millisecond),
		spanEnded(1, 0, "outer", 5*time.Millisecond, 5*time.Millisecond, effect.EventStatusSuccess),
		within(0, effect.EventRuntimeClosed, ""),
	})

	const expected = "outer 5ms success\n" +
		"  inner 2ms typed_failure\n" +
		"    - retry_scheduled inner\n" +
		"  unfinished open\n" +
		"outside every span: 1 event(s)\n"
	if rendered := assembled.Render(); rendered != expected {
		t.Fatalf("unexpected rendering:\n%s\nexpected:\n%s", rendered, expected)
	}
	// Twice, because a rendering a test can compare against is one that does
	// not depend on a map's iteration order.
	if again := assembled.Render(); again != expected {
		t.Fatalf("the same trace rendered differently:\n%s", again)
	}
	if failed := assembled.Failed(); len(failed) != 1 || failed[0].Name != "inner" {
		t.Fatalf("expected the one failed span, got %v", failed)
	}
	if spans := assembled.Spans(); len(spans) != 3 {
		t.Fatalf("expected every span, got %d", len(spans))
	}
}

func TestASpansOwnEventsAndItsChildrenRenderInTheOrderTheyHappened(t *testing.T) {
	// A scope that closed after its children ran must not be printed before
	// them: events first and children second reads as a different program
	// from the one that ran.
	assembled := trace.Assemble([]effect.RuntimeEvent{
		spanStarted(1, 0, "holding", 0),
		earlier(within(1, effect.EventScopeOpened, "holding"), time.Millisecond),
		spanStarted(2, 1, "inside", 2*time.Millisecond),
		spanEnded(2, 1, "inside", 3*time.Millisecond, time.Millisecond, effect.EventStatusSuccess),
		earlier(within(1, effect.EventScopeClosed, "holding"), 4*time.Millisecond),
		spanEnded(1, 0, "holding", 5*time.Millisecond, 5*time.Millisecond, effect.EventStatusSuccess),
	})

	const expected = "holding 5ms success\n" +
		"  - scope_opened holding\n" +
		"  inside 1ms success\n" +
		"  - scope_closed holding\n"
	if rendered := assembled.Render(); rendered != expected {
		t.Fatalf("unexpected rendering:\n%s\nexpected:\n%s", rendered, expected)
	}
}
