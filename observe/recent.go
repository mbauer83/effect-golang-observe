package observe

// A window on what just happened.

import (
	"context"
	"errors"
	"sync"

	"github.com/mbauer83/effect-golang/effect"
)

// Recent keeps the last events and forgets the rest.
//
// A run of any length emits more events than anything wants to hold, so the
// question a debugger actually asks is not "what happened" but "what happened
// just now". A fixed window answers that in bounded memory, which is the only
// way an observer may answer it: this one is installed for the whole life of a
// program, and a slice that grew per event would be the leak.
//
// Seen counts everything, including what has been forgotten, so a reader can
// tell a quiet program from a window that has already turned over.
type Recent struct {
	mutex   sync.Mutex
	held    []effect.RuntimeEvent
	next    int
	wrapped bool
	seen    uint64
}

// Keep makes a window over the last capacity events.
func Keep(capacity int) (*Recent, error) {
	if capacity < 1 {
		return nil, errNoWindow
	}
	return &Recent{held: make([]effect.RuntimeEvent, capacity)}, nil
}

// Observe records the event, replacing the oldest once the window is full.
func (recent *Recent) Observe(_ context.Context, event effect.RuntimeEvent) {
	recent.mutex.Lock()
	defer recent.mutex.Unlock()
	recent.held[recent.next] = event
	recent.next++
	recent.seen++
	if recent.next == len(recent.held) {
		recent.next = 0
		recent.wrapped = true
	}
}

// Events are what the window holds, oldest first.
//
// A copy, because the window keeps being written to and a reader walking the
// live one would see events move under it.
func (recent *Recent) Events() []effect.RuntimeEvent {
	recent.mutex.Lock()
	defer recent.mutex.Unlock()
	if !recent.wrapped {
		return append([]effect.RuntimeEvent{}, recent.held[:recent.next]...)
	}
	ordered := make([]effect.RuntimeEvent, 0, len(recent.held))
	ordered = append(ordered, recent.held[recent.next:]...)
	return append(ordered, recent.held[:recent.next]...)
}

// Seen is how many events have arrived, including those forgotten.
func (recent *Recent) Seen() uint64 {
	recent.mutex.Lock()
	defer recent.mutex.Unlock()
	return recent.seen
}

var errNoWindow = errors.New("observe: a window needs a capacity of at least one")
