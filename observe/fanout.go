package observe

// One event, several places.

import (
	"context"

	"github.com/mbauer83/effect-golang/effect"
)

// Fanout delivers every event to each observer, in the order given.
//
// A runtime takes one observer, and a program that wants a live span tree, a
// metric aggregate and a log line wants three. Composing them here rather than
// asking the runtime for a list keeps the runtime's contract at one observer
// and puts the ordering where it can be reasoned about: first to last, on the
// caller's fiber, so two observers cannot see the same run in two orders.
//
// It flushes what it holds, so a Fanout containing a Buffered is drained by
// Runtime.Close exactly as the Buffered alone would be. A flush failure from
// one does not skip the others; the first is reported and the rest still run,
// because a queue nobody drained is worse than a message nobody read.
func Fanout(observers ...effect.Observer) effect.Observer {
	kept := make([]effect.Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			kept = append(kept, observer)
		}
	}
	return fanout(kept)
}

type fanout []effect.Observer

func (each fanout) Observe(ctx context.Context, event effect.RuntimeEvent) {
	for _, observer := range each {
		observer.Observe(ctx, event)
	}
}

// Flush drains every observer that buffers, and reports the first failure.
func (each fanout) Flush(ctx context.Context) error {
	var first error
	for _, observer := range each {
		buffering, buffers := observer.(effect.Flusher)
		if !buffers {
			continue
		}
		if err := buffering.Flush(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
