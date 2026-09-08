package trace

// Which fibers are running, and how long each has been.
//
// The other structure of what ran. A span is the logical one -- what the
// program said it was doing -- and a fiber is the execution one: what the
// runtime is actually running. They are different questions, and the one a
// program that has stopped responding needs is this one, which is why ZIO's
// Fiber.dump reports a fiber's age and what it is waiting on before it reports
// anything else.
//
// Bounded the same way the span tracker is: a fiber is remembered when it
// starts and forgotten when it completes, so a program that forks a million
// holds none of them. A fiber that never completes is never forgotten, and
// that is the report.

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// Fiber is one fiber the runtime is running.
type Fiber struct {
	ID       uint64
	ParentID uint64
	// Operation is what was running when the fiber was forked, which is the
	// nearest thing to a name a fiber has: the runtime names work, not
	// fibers.
	Operation string
	// SpanID is the span the fork happened in, so a fiber can be found in the
	// trace beside it.
	SpanID  uint64
	Started time.Time
	// Children are the fibers this one forked, oldest first.
	Children []Fiber
}

// Age is how long the fiber has been running.
//
// The first thing to know about a fiber that is still going, and the reason a
// live view is worth more than a count: twelve fibers running is a program
// working, and one fiber running for four minutes is a program stuck.
func (fiber Fiber) Age(now time.Time) time.Duration {
	return now.Sub(fiber.Started)
}

// Fibers is an observer that tracks the fibers currently running.
type Fibers struct {
	mutex   sync.Mutex
	running map[uint64]Fiber
	started uint64
	ended   uint64
}

// WatchFibers makes an observer over the running fibers.
func WatchFibers() *Fibers {
	return &Fibers{running: map[uint64]Fiber{}}
}

// Observe records a fiber starting or completing and ignores everything else.
func (fibers *Fibers) Observe(_ context.Context, event effect.RuntimeEvent) {
	switch event.Kind {
	case effect.EventFiberStarted:
		fibers.begin(event)
	case effect.EventFiberCompleted:
		fibers.finish(event)
	}
}

func (fibers *Fibers) begin(event effect.RuntimeEvent) {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	fibers.running[event.FiberID] = Fiber{
		ID:        event.FiberID,
		ParentID:  event.ParentFiber,
		Operation: event.Operation,
		SpanID:    event.SpanID,
		Started:   event.Timestamp,
	}
	fibers.started++
}

func (fibers *Fibers) finish(event effect.RuntimeEvent) {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	if _, known := fibers.running[event.FiberID]; !known {
		return
	}
	delete(fibers.running, event.FiberID)
	fibers.ended++
}

// Running are the fibers still going, as the tree they were forked in.
//
// A fiber whose parent has already completed is a root here. That is the
// truthful reading rather than a hole in the tree: the parent is gone, this
// one is not, and a program that forks work outliving its forker is doing
// something deliberate.
func (fibers *Fibers) Running() []Fiber {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()

	children := map[uint64][]uint64{}
	roots := make([]uint64, 0, len(fibers.running))
	for identity, fiber := range fibers.running {
		if _, forked := fibers.running[fiber.ParentID]; forked && fiber.ParentID != identity {
			children[fiber.ParentID] = append(children[fiber.ParentID], identity)
			continue
		}
		roots = append(roots, identity)
	}
	fibers.oldestFirst(roots)
	tree := make([]Fiber, 0, len(roots))
	for _, identity := range roots {
		tree = append(tree, fibers.nested(identity, children))
	}
	return tree
}

func (fibers *Fibers) nested(identity uint64, children map[uint64][]uint64) Fiber {
	fiber := fibers.running[identity]
	forked := children[identity]
	fibers.oldestFirst(forked)
	fiber.Children = make([]Fiber, 0, len(forked))
	for _, child := range forked {
		fiber.Children = append(fiber.Children, fibers.nested(child, children))
	}
	return fiber
}

// oldestFirst orders by when each fiber started, so the one that has been
// running longest comes first -- which is the one to look at.
//
// By the runtime's timestamp and not by the order the events arrived: fibers
// that begin at once arrive in whichever order their goroutines got scheduled,
// and a live view that reordered itself between two readings of the same three
// fibers would be unreadable. The identity breaks a tie, as it does for spans.
func (fibers *Fibers) oldestFirst(identities []uint64) {
	slices.SortStableFunc(identities, func(first uint64, second uint64) int {
		if started := fibers.running[first].Started.Compare(
			fibers.running[second].Started); started != 0 {
			return started
		}
		return cmp.Compare(first, second)
	})
}

// Count is how many fibers are running.
func (fibers *Fibers) Count() int {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	return len(fibers.running)
}

// Started and Ended are how many fibers this has seen begin and complete.
func (fibers *Fibers) Started() uint64 {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	return fibers.started
}

func (fibers *Fibers) Ended() uint64 {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	return fibers.ended
}

// Render is the running fibers as a tree of lines, each with its age.
//
// Age first, because that is the question: ZIO prints a fiber's lifetime
// before anything else about it, and for the same reason.
func (fibers *Fibers) Render(now time.Time) string {
	var rendered strings.Builder
	for _, fiber := range fibers.Running() {
		renderFiber(&rendered, fiber, now, 0)
	}
	return rendered.String()
}

func renderFiber(into *strings.Builder, fiber Fiber, now time.Time, depth int) {
	into.WriteString(strings.Repeat("  ", depth))
	into.WriteString("#")
	into.WriteString(strconv.FormatUint(fiber.ID, 10))
	into.WriteString(" running ")
	into.WriteString(fiber.Age(now).String())
	if fiber.Operation != "" {
		into.WriteString(", forked in ")
		into.WriteString(fiber.Operation)
	}
	into.WriteString("\n")
	for _, child := range fiber.Children {
		renderFiber(into, child, now, depth+1)
	}
}
