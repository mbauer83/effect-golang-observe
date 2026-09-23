package trace

// One span, and what it holds.

import (
	"log/slog"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// Span is one WithSpan boundary: what it was, how long it took, how it ended,
// and what happened inside it.
type Span struct {
	// ID and ParentID are the runtime's own identities. ParentID is zero for a
	// span nothing enclosed.
	ID       uint64
	ParentID uint64
	// Name is the span's operation name, and Source the call site the runtime
	// captured when the span was described.
	Name   string
	Source string
	// FiberID is the fiber the span started on. A span's children may run on
	// others, which is what makes the fiber worth keeping beside the span.
	FiberID uint64

	StartTime time.Time
	// EndTime and Duration are the runtime's measurement, not a subtraction of
	// two wall-clock readings. They are zero while the span is open.
	EndTime  time.Time
	Duration time.Duration
	Status   effect.EventStatus

	Attributes []slog.Attr

	// Children are the spans this one enclosed, oldest first.
	Children []Span
	// Events are what happened inside this span and outside every child of
	// it: a resource acquired, a retry scheduled, a log delivered. The
	// span's own start and end are not among them -- they are this span.
	Events []effect.RuntimeEvent
}

// IsOpen says the span started and has not been seen to end.
//
// Which is a report and not a gap: a span still open when a run ended is where
// a hung program is, and the whole reason an unfinished span is kept rather
// than dropped for being incomplete.
func (span Span) IsOpen() bool {
	return span.EndTime.IsZero()
}

// Age is how long an open span has been open, and how long a finished one
// took counted from now -- so a caller reporting live spans has one question
// to ask.
//
// The reason a live view is worth more than a count: twelve spans open is a
// program working, and one span open for four minutes is a program stuck.
// Duration cannot answer it, because the runtime measures a span when it ends
// and an open span has not.
func (span Span) Age(now time.Time) time.Duration {
	if !span.IsOpen() {
		return span.Duration
	}
	return now.Sub(span.StartTime)
}

// IsUnsuccessful says the span ended in something other than success. It is false for
// an open span, which has not ended in anything yet.
func (span Span) IsUnsuccessful() bool {
	switch span.Status {
	case effect.EventStatusFailure, effect.EventStatusDefect,
		effect.EventStatusInterrupted:
		return true
	default:
		return false
	}
}

// Self is how long the span took that its children did not.
//
// What "hot" means. A span of two hundred milliseconds that spent a hundred
// and ninety of them inside one child is not where the time went; the child
// is. Subtracting the children is the whole of the arithmetic a flame graph
// does, and it is the number to rank by.
//
// Zero for an open span, which has no duration to divide, and never negative:
// children that overlap -- work forked and run at once -- can add up to more
// than the parent's wall-clock, and a negative self time would be a strange
// way to report concurrency.
func (span Span) Self() time.Duration {
	if span.IsOpen() {
		return 0
	}
	inside := time.Duration(0)
	for _, child := range span.Children {
		inside += child.Duration
	}
	if inside >= span.Duration {
		return 0
	}
	return span.Duration - inside
}

// Descendants counts the spans beneath this one.
func (span Span) Descendants() int {
	count := 0
	for _, child := range span.Children {
		count += 1 + child.Descendants()
	}
	return count
}
