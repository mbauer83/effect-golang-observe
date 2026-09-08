package unit

// The queue: that it delivers, that Flush is the end of it, and that each
// overflow policy does what its name says.

import (
	"context"
	"sync"
	"testing"

	"github.com/mbauer83/effect-golang-observe/observe"
	"github.com/mbauer83/effect-golang/effect"
)

// counting records under a lock, because a buffer delivers from a goroutine of
// its own and the whole point is that the caller's fiber is not the one
// delivering.
type counting struct {
	mutex sync.Mutex
	seen  []effect.RuntimeEvent
	// entered reports each delivery and release holds the worker inside one.
	// entered is buffered, because the worker reports every delivery and a
	// test only waits for the first: an unbuffered one would hold the worker
	// in a handshake nobody is left to complete.
	entered chan struct{}
	release chan struct{}
}

func (record *counting) Observe(_ context.Context, event effect.RuntimeEvent) {
	if record.entered != nil {
		record.entered <- struct{}{}
		<-record.release
	}
	record.mutex.Lock()
	defer record.mutex.Unlock()
	record.seen = append(record.seen, event)
}

func (record *counting) count() int {
	record.mutex.Lock()
	defer record.mutex.Unlock()
	return len(record.seen)
}

func (record *counting) operations() []string {
	record.mutex.Lock()
	defer record.mutex.Unlock()
	named := make([]string, 0, len(record.seen))
	for _, event := range record.seen {
		named = append(named, event.Operation)
	}
	return named
}

func TestFlushDeliversEverythingQueuedAndEndsTheQueueing(t *testing.T) {
	kept := &counting{}
	buffer, err := observe.Buffer(kept, 64, observe.Block)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 20 {
		buffer.Observe(context.Background(), spanStarted(uint64(index+1), 0, "queued", 0))
	}
	if err := buffer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := kept.count(); got != 20 {
		t.Fatalf("expected everything queued delivered, got %d", got)
	}

	// After the end, an event is delivered where it stands. There is no
	// observed work left to keep fast, and there are still events: a Runtime
	// emits runtime_closed after flushing its capabilities, so a buffer that
	// stopped listening at Flush would never deliver it.
	buffer.Observe(context.Background(), spanStarted(21, 0, "late", 0))
	if got := kept.count(); got != 21 {
		t.Fatalf("expected the late event delivered inline, got %d", got)
	}
	if dropped := buffer.Dropped(); dropped != 0 {
		t.Fatalf("expected nothing dropped by a queue this size, got %d", dropped)
	}
	// And flushing again is nothing, so an owner that also flushes after the
	// runtime did is not an error.
	if err := buffer.Flush(context.Background()); err != nil {
		t.Fatalf("expected a second flush to do nothing, got %v", err)
	}
}

func TestDropNewestKeepsWhatIsQueuedAndCountsWhatItRefused(t *testing.T) {
	// The worker is held inside its first delivery, so the queue is provably
	// full rather than probably full.
	kept := &counting{entered: make(chan struct{}, 8), release: make(chan struct{})}
	buffer, err := observe.Buffer(kept, 2, observe.DropNewest)
	if err != nil {
		t.Fatal(err)
	}
	buffer.Observe(context.Background(), spanStarted(1, 0, "first", 0))
	<-kept.entered

	buffer.Observe(context.Background(), spanStarted(2, 0, "second", 0))
	buffer.Observe(context.Background(), spanStarted(3, 0, "third", 0))
	buffer.Observe(context.Background(), spanStarted(4, 0, "refused", 0))
	if dropped := buffer.Dropped(); dropped != 1 {
		t.Fatalf("expected the arriving event dropped, got %d", dropped)
	}

	close(kept.release)
	if err := buffer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := kept.operations(); len(got) != 3 || got[2] != "third" {
		t.Fatalf("expected the queued three and not the fourth, got %v", got)
	}
}

func TestDropOldestMakesRoomForWhatJustHappened(t *testing.T) {
	// The other way round, and the reason both exist: a window on what is
	// happening now wants the recent events, not the first ones it ever saw.
	kept := &counting{entered: make(chan struct{}, 8), release: make(chan struct{})}
	buffer, err := observe.Buffer(kept, 2, observe.DropOldest)
	if err != nil {
		t.Fatal(err)
	}
	buffer.Observe(context.Background(), spanStarted(1, 0, "delivering", 0))
	<-kept.entered

	buffer.Observe(context.Background(), spanStarted(2, 0, "oldest", 0))
	buffer.Observe(context.Background(), spanStarted(3, 0, "middle", 0))
	buffer.Observe(context.Background(), spanStarted(4, 0, "newest", 0))
	if dropped := buffer.Dropped(); dropped != 1 {
		t.Fatalf("expected one event dropped to make room, got %d", dropped)
	}

	close(kept.release)
	if err := buffer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := kept.operations()
	if len(got) != 3 || got[1] != "middle" || got[2] != "newest" {
		t.Fatalf("expected the oldest queued one dropped, got %v", got)
	}
}

func TestABufferNeedsSomewhereToDeliverAndSomewhereToHold(t *testing.T) {
	if _, err := observe.Buffer(nil, 4, observe.Block); err == nil {
		t.Error("expected a buffer with no observer to be refused")
	}
	if _, err := observe.Buffer(&counting{}, 0, observe.Block); err == nil {
		t.Error("expected a buffer with no capacity to be refused")
	}
}

func TestDeliveryOutlivesTheCancellationOfTheFiberThatEmitted(t *testing.T) {
	// An interrupted run is when the events matter most, and its context is
	// cancelled by the time a queue drains. An exporter handed that context
	// would abandon exactly those events.
	kept := &noting{}
	buffer, err := observe.Buffer(observerOf(func(ctx context.Context, event effect.RuntimeEvent) {
		if ctx.Err() != nil {
			t.Errorf("delivered with a cancelled context: %v", ctx.Err())
		}
		kept.Observe(ctx, event)
	}), 4, observe.Block)
	if err != nil {
		t.Fatal(err)
	}

	cancelled, stop := context.WithCancel(context.Background())
	buffer.Observe(cancelled, spanStarted(1, 0, "emitted", 0))
	stop()

	if err := buffer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(kept.seen) != 1 {
		t.Fatalf("expected the event delivered, got %d", len(kept.seen))
	}
}

// observerOf is an observer from a function, for a test that only wants one.
type observerFunc func(context.Context, effect.RuntimeEvent)

func (observing observerFunc) Observe(ctx context.Context, event effect.RuntimeEvent) {
	observing(ctx, event)
}

func observerOf(observing func(context.Context, effect.RuntimeEvent)) effect.Observer {
	return observerFunc(observing)
}
