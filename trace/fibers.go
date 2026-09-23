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
	SpanID    uint64
	StartTime time.Time
	// Children are the fibers this one forked, oldest first.
	Children []Fiber
}

// Age is how long the fiber has been running.
//
// The first thing to know about a fiber that is still going, and the reason a
// live view is worth more than a count: twelve fibers running is a program
// working, and one fiber running for four minutes is a program stuck.
func (fiber Fiber) Age(now time.Time) time.Duration {
	return now.Sub(fiber.StartTime)
}

// Fibers is an observer that tracks the fibers currently running.
type Fibers struct {
	mutex  sync.Mutex
	live   map[uint64]Fiber
	starts uint64
	ends   uint64
}

// NewFibers makes an observer over the running fibers.
func NewFibers() *Fibers {
	return &Fibers{live: map[uint64]Fiber{}}
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
	fibers.live[event.FiberID] = Fiber{
		ID:        event.FiberID,
		ParentID:  event.ParentFiber,
		Operation: event.Operation,
		SpanID:    event.SpanID,
		StartTime: event.Timestamp,
	}
	fibers.starts++
}

func (fibers *Fibers) finish(event effect.RuntimeEvent) {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	if _, known := fibers.live[event.FiberID]; !known {
		return
	}
	delete(fibers.live, event.FiberID)
	fibers.ends++
}

// Tree are the fibers still going, as the tree they were forked in.
//
// A fiber whose parent has already completed is a root here. That is the
// truthful reading rather than a hole in the tree: the parent is gone, this
// one is not, and a program that forks work outliving its forker is doing
// something deliberate.
func (fibers *Fibers) Tree() []Fiber {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()

	children := map[uint64][]uint64{}
	roots := make([]uint64, 0, len(fibers.live))
	for identity, fiber := range fibers.live {
		if _, forked := fibers.live[fiber.ParentID]; forked && fiber.ParentID != identity {
			children[fiber.ParentID] = append(children[fiber.ParentID], identity)
			continue
		}
		roots = append(roots, identity)
	}
	fibers.sortOldestFirst(roots)
	tree := make([]Fiber, 0, len(roots))
	for _, identity := range roots {
		tree = append(tree, fibers.subtree(identity, children))
	}
	return tree
}

func (fibers *Fibers) subtree(identity uint64, children map[uint64][]uint64) Fiber {
	fiber := fibers.live[identity]
	childIDs := children[identity]
	fibers.sortOldestFirst(childIDs)
	fiber.Children = make([]Fiber, 0, len(childIDs))
	for _, child := range childIDs {
		fiber.Children = append(fiber.Children, fibers.subtree(child, children))
	}
	return fiber
}

// sortOldestFirst orders by when each fiber started, so the one that has been
// running longest comes first -- which is the one to look at.
//
// By the runtime's timestamp and not by the order the events arrived: fibers
// that begin at once arrive in whichever order their goroutines got scheduled,
// and a live view that reordered itself between two readings of the same three
// fibers would be unreadable. The identity breaks a tie, as it does for spans.
func (fibers *Fibers) sortOldestFirst(identities []uint64) {
	slices.SortStableFunc(identities, func(first uint64, second uint64) int {
		if order := fibers.live[first].StartTime.Compare(
			fibers.live[second].StartTime); order != 0 {
			return order
		}
		return cmp.Compare(first, second)
	})
}

// Count is how many fibers are running.
func (fibers *Fibers) Count() int {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	return len(fibers.live)
}

// Starts and Ends are how many fibers this has seen begin and complete.
func (fibers *Fibers) Starts() uint64 {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	return fibers.starts
}

func (fibers *Fibers) Ends() uint64 {
	fibers.mutex.Lock()
	defer fibers.mutex.Unlock()
	return fibers.ends
}

// Render is the running fibers as a tree of lines, each with its age.
//
// Age first, because that is the question: ZIO prints a fiber's lifetime
// before anything else about it, and for the same reason.
func (fibers *Fibers) Render(now time.Time) string {
	var text strings.Builder
	for _, fiber := range fibers.Tree() {
		renderFiber(&text, fiber, now, 0)
	}
	return text.String()
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
