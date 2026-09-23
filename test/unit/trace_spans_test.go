package unit

// What is open right now: kept while it runs, forgotten when it ends.

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

func TestOpenSpansAreKeptAndEndedOnesAreForgotten(t *testing.T) {
	// The invariant that makes this installable for the life of a program: a
	// span costs memory while it runs and nothing afterwards.
	spans := trace.NewSpans()
	background := context.Background()

	spans.Observe(background, spanStarted(1, 0, "first", 0))
	spans.Observe(background, spanStarted(2, 1, "second", time.Millisecond))
	if count := spans.Count(); count != 2 {
		t.Fatalf("expected both open, got %d", count)
	}

	spans.Observe(background,
		spanEnded(1, 0, "first", 2*time.Millisecond, 2*time.Millisecond, effect.EventStatusSuccess))
	open := spans.Open()
	if len(open) != 1 || open[0].Name != "second" {
		t.Fatalf("expected only the one still running, got %v", open)
	}
	// A span this holds has not been seen to end, so it is open by
	// construction rather than by inspection.
	if !open[0].IsOpen() {
		t.Error("expected a held span to be open")
	}
	if started, ended := spans.Starts(), spans.Ends(); started != 2 || ended != 1 {
		t.Fatalf("expected two started and one ended, got %d and %d", started, ended)
	}
}

func TestOpenSpansComeBackInTheOrderTheyWereOpened(t *testing.T) {
	// A map has no order, and a tool listing what is running wants the oldest
	// first: that is the one that has been running too long.
	spans := trace.NewSpans()
	for index := range 6 {
		spans.Observe(context.Background(),
			spanStarted(uint64(index+1), 0, string(rune('a'+index)), 0))
	}

	open := spans.Open()
	for index, span := range open {
		if span.Name != string(rune('a'+index)) {
			t.Fatalf("expected the opening order, got %v", spanNames(open))
		}
	}
}

func TestAnEndForASpanItNeverSawStartChangesNothing(t *testing.T) {
	// What an observer installed mid-run sees. There is nothing to forget, and
	// counting it as ended would make Started and Ended disagree about a span
	// this never had.
	spans := trace.NewSpans()
	spans.Observe(context.Background(),
		spanEnded(9, 0, "elsewhere", 0, time.Millisecond, effect.EventStatusSuccess))

	if count := spans.Count(); count != 0 {
		t.Fatalf("expected nothing held, got %d", count)
	}
	if ended := spans.Ends(); ended != 0 {
		t.Fatalf("expected no span counted as ended, got %d", ended)
	}
}

func TestEverythingThatIsNotASpanBoundaryIsIgnored(t *testing.T) {
	spans := trace.NewSpans()
	spans.Observe(context.Background(), eventIn(1, effect.EventResourceAcquired, "held"))
	spans.Observe(context.Background(), eventIn(1, effect.EventLogEmitted, "said"))

	if count := spans.Count(); count != 0 {
		t.Fatalf("expected no spans from events that are not span boundaries, got %d", count)
	}
}

func spanNames(spans []trace.Span) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}
	return names
}
