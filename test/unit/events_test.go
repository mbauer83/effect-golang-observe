package unit

// Events built by hand, for the tests that are about reading them.
//
// A runtime emits the real thing and the acceptance suite uses one; here the
// point is the reading, so the events are stated rather than provoked -- which
// is what lets a test state the case it cares about, including the ones a
// runtime does not produce on demand: a span that never ended, an end whose
// start was truncated away.

import (
	"log/slog"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// origin is a fixed instant, so every test's timestamps are comparable and
// none of them depends on when it ran.
var origin = time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)

func at(offset time.Duration) time.Time { return origin.Add(offset) }

func spanStarted(id uint64, parent uint64, name string, offset time.Duration) effect.RuntimeEvent {
	return effect.RuntimeEvent{
		Kind:      effect.EventSpanStarted,
		Timestamp: at(offset),
		Operation: name,
		SpanID:    id,
		ParentID:  parent,
	}
}

func spanEnded(
	id uint64,
	parent uint64,
	name string,
	offset time.Duration,
	duration time.Duration,
	status effect.EventStatus,
) effect.RuntimeEvent {
	return effect.RuntimeEvent{
		Kind:      effect.EventSpanEnded,
		Timestamp: at(offset),
		Duration:  duration,
		Operation: name,
		SpanID:    id,
		ParentID:  parent,
		Status:    status,
	}
}

func eventIn(id uint64, kind effect.EventKind, name string) effect.RuntimeEvent {
	return effect.RuntimeEvent{Kind: kind, Timestamp: at(0), Operation: name, SpanID: id}
}

func withAttributes(event effect.RuntimeEvent, attributes ...slog.Attr) effect.RuntimeEvent {
	event.Attributes = attributes
	return event
}

// operationsOf names the operations of a sequence of events, which is what a
// test comparing order actually wants to read in a failure message.
func operationsOf(events []effect.RuntimeEvent) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.Operation)
	}
	return names
}

// backdate puts an event at a stated offset from the origin, for the tests that
// are about when things happened rather than what they were.
func backdate(event effect.RuntimeEvent, offset time.Duration) effect.RuntimeEvent {
	event.Timestamp = at(offset)
	return event
}
