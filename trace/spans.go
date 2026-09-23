package trace

// What is open right now.

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// Spans is an observer that keeps the spans currently open.
//
// It is bounded by what a program is doing rather than by how long it has been
// doing it: a span is remembered when it starts and forgotten when it ends, so
// a process that opens and closes a million spans holds none of them. That is
// what makes it safe to install for the life of a program -- which is when the
// question "what is it doing now" is actually asked.
//
// A span that never ends is never forgotten, and that is the report rather than
// a leak: something is still running, and this is where it says so. A program
// that truly leaks spans grows here too, in proportion to the leak.
//
// It does not accumulate the events inside a span. Those are unbounded in a
// long-lived span, and the collection that does hold them -- a window kept by
// observe.NewRecent, folded by Assemble -- is bounded by the window instead.
type Spans struct {
	mutex sync.Mutex
	open  map[uint64]Span
	// order is the sequence spans were opened in, kept so two spans opened in
	// the same instant still have a stable order.
	order  map[uint64]uint64
	next   uint64
	starts uint64
	ends   uint64
}

// NewSpans makes an observer over the open spans.
func NewSpans() *Spans {
	return &Spans{open: map[uint64]Span{}, order: map[uint64]uint64{}}
}

// Observe records a span opening or closing and ignores everything else.
func (spans *Spans) Observe(_ context.Context, event effect.RuntimeEvent) {
	switch event.Kind {
	case effect.EventSpanStarted:
		spans.begin(event)
	case effect.EventSpanEnded:
		spans.finish(event)
	}
}

func (spans *Spans) begin(event effect.RuntimeEvent) {
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	spans.open[event.SpanID] = *newSpan(event)
	spans.order[event.SpanID] = spans.next
	spans.next++
	spans.starts++
}

// finish forgets the span. An end for a span this never saw start -- which an
// observer installed mid-run will see -- is counted and otherwise ignored,
// because there is nothing to forget and no half-span worth inventing.
func (spans *Spans) finish(event effect.RuntimeEvent) {
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	if _, known := spans.open[event.SpanID]; !known {
		return
	}
	delete(spans.open, event.SpanID)
	delete(spans.order, event.SpanID)
	spans.ends++
}

// Open are the spans still running, in the order they were opened.
//
// Each is open by construction: EndTime and Duration are zero, because a span
// this still holds has not been seen to end.
func (spans *Spans) Open() []Span {
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	open := make([]Span, 0, len(spans.open))
	for _, span := range spans.open {
		open = append(open, span)
	}
	slices.SortStableFunc(open, func(first Span, second Span) int {
		return cmp.Compare(spans.order[first.ID], spans.order[second.ID])
	})
	return open
}

// Count is how many spans are open.
func (spans *Spans) Count() int {
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	return len(spans.open)
}

// Starts and Ends are how many spans this has seen begin and end. A gap
// between them that does not close is the same report Open makes, in one
// number that a metric can carry.
func (spans *Spans) Starts() uint64 {
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	return spans.starts
}

func (spans *Spans) Ends() uint64 {
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	return spans.ends
}
