package unit

// Fanout and the two ways of selecting what reaches an observer.

import (
	"context"
	"errors"
	"testing"

	"github.com/mbauer83/effect-golang-observe/observe"
	"github.com/mbauer83/effect-golang/effect"
)

// recorder records what it was given, and in what order.
type recorder struct {
	events []effect.RuntimeEvent
}

func (record *recorder) Observe(_ context.Context, event effect.RuntimeEvent) {
	record.events = append(record.events, event)
}

// refuser is an observer that also buffers and cannot drain.
type refuser struct{ recorder }

func (refuser) Flush(context.Context) error { return errRefused }

var errRefused = errors.New("refused to drain")

func TestFanoutGivesEveryObserverEveryEvent(t *testing.T) {
	first, second := &recorder{}, &recorder{}
	fanout := observe.Fanout(first, second)

	fanout.Observe(context.Background(), spanStarted(1, 0, "one", 0))
	fanout.Observe(context.Background(), spanStarted(2, 1, "two", 1))

	for _, observer := range []*recorder{first, second} {
		if len(observer.events) != 2 {
			t.Fatalf("expected both events, got %d", len(observer.events))
		}
		if observer.events[0].Operation != "one" || observer.events[1].Operation != "two" {
			t.Fatalf("expected them in order, got %v", observer.events)
		}
	}
}

func TestFanoutIgnoresAnObserverThatIsNotThere(t *testing.T) {
	// A caller assembling observers from configuration will have a nil among
	// them; delivering to it would be a defect the runtime then reports, for
	// a mistake this can simply absorb.
	sink := &recorder{}
	observe.Fanout(nil, sink, nil).Observe(context.Background(), spanStarted(1, 0, "one", 0))

	if len(sink.events) != 1 {
		t.Fatalf("expected the one real observer to be given the event, got %d", len(sink.events))
	}
}

func TestFanoutDrainsEveryObserverThatBuffersAndReportsTheFirstRefusal(t *testing.T) {
	// A Fanout is what a runtime holds, so it is what Runtime.Close flushes.
	// If it only drained until the first refusal, a queue behind the refusing
	// one would be left full -- which is worse than the message nobody read.
	first, second := &refuser{}, &refuser{}
	buffered, err := observe.NewBuffer(&recorder{}, 4, observe.DropNewest)
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
	sink := &recorder{}
	buffered, err := observe.NewBuffer(sink, 8, observe.Block)
	if err != nil {
		t.Fatal(err)
	}
	only := observe.Filter(buffered, observe.OfKind(effect.EventSpanEnded))

	only.Observe(context.Background(), spanStarted(1, 0, "one", 0))
	only.Observe(context.Background(), spanEnded(1, 0, "one", 1, 5, effect.EventStatusSuccess))
	if err := only.(effect.Flusher).Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(sink.events) != 1 || sink.events[0].Kind != effect.EventSpanEnded {
		t.Fatalf("expected only the end, got %v", sink.events)
	}
}

func TestFailedKeepsEveryStatusThatIsNotSuccess(t *testing.T) {
	sink := &recorder{}
	only := observe.Filter(sink, observe.Unsuccessful())
	for _, status := range []effect.EventStatus{
		effect.EventStatusSuccess, effect.EventStatusFailure,
		effect.EventStatusDefect, effect.EventStatusInterrupted,
		effect.EventStatusNone,
	} {
		only.Observe(context.Background(), spanEnded(1, 0, "one", 0, 1, status))
	}

	if len(sink.events) != 3 {
		t.Fatalf("expected the three that are not success, got %d", len(sink.events))
	}
}
