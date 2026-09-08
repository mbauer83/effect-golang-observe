package unit

// Fanout and the two ways of selecting what reaches an observer.

import (
	"context"
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang-observe/observe"
	"github.com/mbauer83/effect-golang/effect"
)

// noting records what it was given, and in what order.
type noting struct {
	seen []effect.RuntimeEvent
}

func (record *noting) Observe(_ context.Context, event effect.RuntimeEvent) {
	record.seen = append(record.seen, event)
}

// refusing is an observer that also buffers and cannot drain.
type refusing struct{ noting }

func (refusing) Flush(context.Context) error { return errRefused }

var errRefused = errors.New("refused to drain")

func TestFanoutGivesEveryObserverEveryEvent(t *testing.T) {
	first, second := &noting{}, &noting{}
	fanout := observe.Fanout(first, second)

	fanout.Observe(context.Background(), spanStarted(1, 0, "one", 0))
	fanout.Observe(context.Background(), spanStarted(2, 1, "two", 1))

	for _, observer := range []*noting{first, second} {
		if len(observer.seen) != 2 {
			t.Fatalf("expected both events, got %d", len(observer.seen))
		}
		if observer.seen[0].Operation != "one" || observer.seen[1].Operation != "two" {
			t.Fatalf("expected them in order, got %v", observer.seen)
		}
	}
}

func TestFanoutIgnoresAnObserverThatIsNotThere(t *testing.T) {
	// A caller assembling observers from configuration will have a nil among
	// them; delivering to it would be a defect the runtime then reports, for
	// a mistake this can simply absorb.
	kept := &noting{}
	observe.Fanout(nil, kept, nil).Observe(context.Background(), spanStarted(1, 0, "one", 0))

	if len(kept.seen) != 1 {
		t.Fatalf("expected the one real observer to be given the event, got %d", len(kept.seen))
	}
}

func TestFanoutDrainsEveryObserverThatBuffersAndReportsTheFirstRefusal(t *testing.T) {
	// A Fanout is what a runtime holds, so it is what Runtime.Close flushes.
	// If it only drained until the first refusal, a queue behind the refusing
	// one would be left full -- which is worse than the message nobody read.
	first, second := &refusing{}, &refusing{}
	buffered, err := observe.Buffer(&noting{}, 4, observe.DropNewest)
	if err != nil {
		t.Fatal(err)
	}
	fanout := observe.Fanout(first, buffered, second)

	// The runtime discovers a flusher by asking, which is what the port says;
	// a caller holding one asks the same way.
	drains, buffers := fanout.(effect.Flusher)
	if !buffers {
		t.Fatal("expected a fanout holding a buffer to be drainable")
	}
	if err := drains.Flush(context.Background()); !errors.Is(err, errRefused) {
		t.Fatalf("expected the first refusal reported, got %v", err)
	}
	// The one that could drain did, which a second flush proves: a drained
	// buffer flushes again without complaint.
	if err := buffered.Flush(context.Background()); err != nil {
		t.Fatalf("expected the buffer already drained, got %v", err)
	}
}

func TestOnlyTheKindsAskedForArriveAndFlushStillReachesTheBufferBehind(t *testing.T) {
	kept := &noting{}
	buffered, err := observe.Buffer(kept, 8, observe.Block)
	if err != nil {
		t.Fatal(err)
	}
	only := observe.Filtered(buffered, observe.OfKind(effect.EventSpanEnded))

	only.Observe(context.Background(), spanStarted(1, 0, "one", 0))
	only.Observe(context.Background(), spanEnded(1, 0, "one", 1, 5, effect.EventStatusSuccess))
	if err := only.(effect.Flusher).Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(kept.seen) != 1 || kept.seen[0].Kind != effect.EventSpanEnded {
		t.Fatalf("expected only the end, got %v", kept.seen)
	}
}

func TestFailedKeepsEveryStatusThatIsNotSuccess(t *testing.T) {
	kept := &noting{}
	only := observe.Filtered(kept, observe.Failed())
	for _, status := range []effect.EventStatus{
		effect.EventStatusSuccess, effect.EventStatusFailure,
		effect.EventStatusDefect, effect.EventStatusInterrupted,
		effect.EventStatusNone,
	} {
		only.Observe(context.Background(), spanEnded(1, 0, "one", 0, 1, status))
	}

	if len(kept.seen) != 3 {
		t.Fatalf("expected the three that are not success, got %d", len(kept.seen))
	}
}
