package trace

// What is open right now.

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// Running is an observer that keeps the spans currently open.
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
// observe.Keep, folded by Assemble -- is bounded by the window instead.
type Running struct {
	mutex sync.Mutex
	open  map[uint64]Span
	// order is the sequence spans were opened in, kept so two spans opened in
	// the same instant still have a stable order.
	order   map[uint64]uint64
	opened  uint64
	started uint64
	ended   uint64
}

// Watch makes an observer over the open spans.
func Watch() *Running {
	return &Running{open: map[uint64]Span{}, order: map[uint64]uint64{}}
}

// Observe records a span opening or closing and ignores everything else.
func (running *Running) Observe(_ context.Context, event effect.RuntimeEvent) {
	switch event.Kind {
	case effect.EventSpanStarted:
		running.begin(event)
	case effect.EventSpanEnded:
		running.finish(event)
	}
}

func (running *Running) begin(event effect.RuntimeEvent) {
	running.mutex.Lock()
	defer running.mutex.Unlock()
	running.open[event.SpanID] = *startedSpan(event)
	running.order[event.SpanID] = running.opened
	running.opened++
	running.started++
}

// finish forgets the span. An end for a span this never saw start -- which an
// observer installed mid-run will see -- is counted and otherwise ignored,
// because there is nothing to forget and no half-span worth inventing.
func (running *Running) finish(event effect.RuntimeEvent) {
	running.mutex.Lock()
	defer running.mutex.Unlock()
	if _, known := running.open[event.SpanID]; !known {
		return
	}
	delete(running.open, event.SpanID)
	delete(running.order, event.SpanID)
	running.ended++
}

// Open are the spans still running, in the order they were opened.
//
// Each is open by construction: Ended and Duration are zero, because a span
// this still holds has not been seen to end.
func (running *Running) Open() []Span {
	running.mutex.Lock()
	defer running.mutex.Unlock()
	held := make([]Span, 0, len(running.open))
	for _, span := range running.open {
		held = append(held, span)
	}
	slices.SortStableFunc(held, func(first Span, second Span) int {
		return cmp.Compare(running.order[first.ID], running.order[second.ID])
	})
	return held
}

// Count is how many spans are open.
func (running *Running) Count() int {
	running.mutex.Lock()
	defer running.mutex.Unlock()
	return len(running.open)
}

// Started and Ended are how many spans this has seen begin and end. A gap
// between them that does not close is the same report Open makes, in one
// number that a metric can carry.
func (running *Running) Started() uint64 {
	running.mutex.Lock()
	defer running.mutex.Unlock()
	return running.started
}

func (running *Running) Ended() uint64 {
	running.mutex.Lock()
	defer running.mutex.Unlock()
	return running.ended
}
