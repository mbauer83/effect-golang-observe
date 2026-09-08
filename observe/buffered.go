package observe

// Delivery off the fiber that caused the event.
//
// The runtime calls an observer inline, so anything slow -- a network export,
// a disk write, a lock somebody else holds -- is paid by the work being
// observed. A queue moves that cost, and a queue has exactly three questions
// nobody can answer generically: how large, what happens when it is full, and
// who drains it. This answers them explicitly rather than choosing for the
// caller.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/mbauer83/effect-golang/effect"
)

// Overflow is what a full queue does with the event that arrives next.
//
// Three named policies rather than a flag, because "true" does not say which
// of the three it meant, and the choice is a real one: a metric aggregate
// would rather lose the newest, a debugger reading a window would rather lose
// the oldest, and a run whose events must all arrive would rather wait.
type Overflow uint8

const (
	// DropNewest discards the arriving event. The emitting fiber never waits,
	// and what is already queued is what gets delivered.
	DropNewest Overflow = iota
	// DropOldest discards the longest-waiting event to make room. A window on
	// what is happening now wants this: the recent events are the useful ones.
	DropOldest
	// Block makes the emitting fiber wait for room, so nothing is lost and the
	// observed work is slowed instead. It is the honest choice when the events
	// are the point -- an audit trail -- and the wrong one otherwise.
	Block
)

// Buffered hands events to another observer from a goroutine of its own.
//
// It owns that goroutine, and Flush is what ends it: the queue is drained,
// the worker stops, and a second Flush does nothing. A Runtime calls Flush at
// Close for any observer that buffers, so installing one is enough to have it
// drained; an owner who never installs it must call Flush itself or the
// goroutine outlives the program's interest in it.
//
// An event arriving after Flush is delivered on the caller's fiber rather than
// queued or dropped. The queue exists to keep observed work fast, and once the
// queue has been drained there is no observed work left to keep fast -- while
// there are still events: a Runtime emits runtime_closed after it has flushed
// its capabilities, so a buffer that treated Flush as the end could never
// deliver the event that says the runtime closed.
type Buffered struct {
	observer effect.Observer
	overflow Overflow
	queue    chan queued
	drained  chan struct{}
	dropped  atomic.Uint64

	// mutex keeps a producer from sending into a queue Flush is closing. It is
	// read-held for the duration of one offer, so Flush cannot close the queue
	// while an event is in flight -- and a producer blocked on a full queue
	// under Block delays Flush only until the worker makes room.
	mutex   sync.RWMutex
	stopped bool
}

// queued keeps each event with the context it happened in, stripped of
// cancellation.
//
// The emitting fiber's context may well be cancelled by the time the queue
// drains -- an interrupted run is exactly when the events matter most -- and an
// exporter handed a cancelled context would abandon them. Its values are kept,
// so an adapter correlating through the context still can.
type queued struct {
	ctx   context.Context
	event effect.RuntimeEvent
}

// Buffer makes a buffering observer over another.
func Buffer(observer effect.Observer, capacity int, overflow Overflow) (*Buffered, error) {
	if observer == nil {
		return nil, errNoObserver
	}
	if capacity < 1 {
		return nil, errNoCapacity
	}
	buffer := &Buffered{
		observer: observer,
		overflow: overflow,
		queue:    make(chan queued, capacity),
		drained:  make(chan struct{}),
	}
	go buffer.deliver()
	return buffer, nil
}

// Observe queues the event, or applies the overflow policy to it -- or, once
// the queue has been drained, delivers it where it stands.
func (buffer *Buffered) Observe(ctx context.Context, event effect.RuntimeEvent) {
	buffer.mutex.RLock()
	defer buffer.mutex.RUnlock()
	if buffer.stopped {
		buffer.observer.Observe(ctx, event)
		return
	}
	buffer.offer(queued{ctx: context.WithoutCancel(ctx), event: event})
}

// Dropped is how many events the overflow policy discarded.
//
// A buffered pipeline that loses events silently is the failure this exists to
// prevent: the number is the only evidence, so it is readable rather than
// logged, and a caller that reports it has an honest account of its own
// telemetry.
func (buffer *Buffered) Dropped() uint64 {
	return buffer.dropped.Load()
}

// Flush drains what is queued and stops the worker.
func (buffer *Buffered) Flush(ctx context.Context) error {
	buffer.mutex.Lock()
	if buffer.stopped {
		buffer.mutex.Unlock()
		return nil
	}
	buffer.stopped = true
	close(buffer.queue)
	buffer.mutex.Unlock()

	select {
	case <-buffer.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (buffer *Buffered) offer(held queued) {
	if buffer.overflow == Block {
		buffer.queue <- held
		return
	}
	select {
	case buffer.queue <- held:
		return
	default:
	}
	if buffer.overflow == DropOldest {
		// One event out, one in. A second failure to send means the worker
		// filled it again in between, which is a queue under real pressure and
		// not a race worth looping over.
		select {
		case <-buffer.queue:
			buffer.dropped.Add(1)
		default:
		}
		select {
		case buffer.queue <- held:
			return
		default:
		}
	}
	buffer.dropped.Add(1)
}

func (buffer *Buffered) deliver() {
	defer close(buffer.drained)
	for held := range buffer.queue {
		buffer.observer.Observe(held.ctx, held.event)
	}
}

var (
	errNoObserver = errors.New("observe: a buffer needs an observer to deliver to")
	errNoCapacity = errors.New("observe: a buffer needs a capacity of at least one")
)
