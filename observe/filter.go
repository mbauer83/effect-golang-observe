package observe

// Which events reach an observer.

import (
	"context"

	"github.com/mbauer83/effect-golang/effect"
)

// Predicate decides whether one event is delivered.
//
// A function rather than a set of kinds, because the useful questions are not
// all about the kind: a tool watching one operation, or only what failed, or
// only what took longer than a threshold, all ask something a list of kinds
// cannot answer. OfKind composes the common case out of it.
type Predicate func(effect.RuntimeEvent) bool

// Filter delivers only the events the predicate keeps.
//
// It is where the cost of observing is decided. Every event the runtime emits
// crosses this on the fiber that emitted it, so a predicate that rejects early
// is what makes an expensive observer affordable -- and rejecting here rather
// than inside the observer means the observer never has to know it is being
// sampled.
func Filter(observer effect.Observer, predicate Predicate) effect.Observer {
	if observer == nil || predicate == nil {
		return Fanout()
	}
	return filter{observer: observer, predicate: predicate}
}

type filter struct {
	observer  effect.Observer
	predicate Predicate
}

func (only filter) Observe(ctx context.Context, event effect.RuntimeEvent) {
	if only.predicate(event) {
		only.observer.Observe(ctx, event)
	}
}

// Flush drains the observer behind the filter. A filter drops events; it does
// not change whether what got through still has to be delivered.
func (only filter) Flush(ctx context.Context) error {
	flusher, buffers := only.observer.(effect.Flusher)
	if !buffers {
		return nil
	}
	return flusher.Flush(ctx)
}

// OfKind keeps the events whose kind is one of those named.
//
// The kinds are a bounded vocabulary, which is what makes this safe to build a
// map from: an operation name or an attribute value is unbounded and would
// make the map the memory leak.
func OfKind(kinds ...effect.EventKind) Predicate {
	members := make(map[effect.EventKind]bool, len(kinds))
	for _, kind := range kinds {
		members[kind] = true
	}
	return func(event effect.RuntimeEvent) bool { return members[event.Kind] }
}

// Unsuccessful keeps the events whose status says the work did not succeed.
//
// The three that are not success are separate statuses on purpose -- a defect
// is not a typed failure and neither is an interruption -- so a tool that wants
// one of them says which. This is for the tool that wants all three.
func Unsuccessful() Predicate {
	return func(event effect.RuntimeEvent) bool {
		switch event.Status {
		case effect.EventStatusFailure, effect.EventStatusDefect,
			effect.EventStatusInterrupted:
			return true
		default:
			return false
		}
	}
}
