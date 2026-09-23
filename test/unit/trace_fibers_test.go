package unit

// The execution structure: which fibers are running, nested as they were
// forked, and how long each has been going.

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

func fiberStarted(id uint64, parent uint64, operation string, offset time.Duration) effect.RuntimeEvent {
	return effect.RuntimeEvent{
		Kind:        effect.EventFiberStarted,
		Timestamp:   at(offset),
		Operation:   operation,
		FiberID:     id,
		ParentFiber: parent,
	}
}

func fiberCompleted(id uint64, parent uint64, offset time.Duration) effect.RuntimeEvent {
	return effect.RuntimeEvent{
		Kind:        effect.EventFiberCompleted,
		Timestamp:   at(offset),
		FiberID:     id,
		ParentFiber: parent,
		Status:      effect.EventStatusSuccess,
	}
}

func TestRunningFibersAreNestedAsTheyWereForked(t *testing.T) {
	fibers := trace.NewFibers()
	background := context.Background()

	fibers.Observe(background, fiberStarted(1, 0, "serve", 0))
	fibers.Observe(background, fiberStarted(2, 1, "handle", time.Millisecond))
	fibers.Observe(background, fiberStarted(3, 1, "handle", 2*time.Millisecond))

	tree := fibers.Tree()
	if len(tree) != 1 || tree[0].ID != 1 {
		t.Fatalf("expected the forker as the one root, got %v", tree)
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("expected both forked fibers under it, got %v", tree[0].Children)
	}
	// Oldest first: the one that has been running longest is the one to look
	// at.
	if tree[0].Children[0].ID != 2 || tree[0].Children[1].ID != 3 {
		t.Fatalf("expected them in the order they were forked, got %v", tree[0].Children)
	}
	if fibers.Count() != 3 {
		t.Fatalf("expected three running, got %d", fibers.Count())
	}
}

func TestAFiberIsForgottenWhenItCompletes(t *testing.T) {
	fibers := trace.NewFibers()
	background := context.Background()

	fibers.Observe(background, fiberStarted(1, 0, "serve", 0))
	fibers.Observe(background, fiberStarted(2, 1, "handle", time.Millisecond))
	fibers.Observe(background, fiberCompleted(2, 1, 2*time.Millisecond))

	if fibers.Count() != 1 {
		t.Fatalf("expected the completed fiber forgotten, got %d running", fibers.Count())
	}
	if started, ended := fibers.Starts(), fibers.Ends(); started != 2 || ended != 1 {
		t.Fatalf("expected two started and one ended, got %d and %d", started, ended)
	}
	// A completion for a fiber this never saw start changes nothing: there is
	// nothing to forget, and counting it would make the two numbers disagree
	// about a fiber this never had.
	fibers.Observe(background, fiberCompleted(9, 0, 3*time.Millisecond))
	if ended := fibers.Ends(); ended != 1 {
		t.Fatalf("expected the unknown completion ignored, got %d", ended)
	}
}

func TestAFiberOutlivingItsForkerIsARootRatherThanAHoleInTheTree(t *testing.T) {
	fibers := trace.NewFibers()
	background := context.Background()

	fibers.Observe(background, fiberStarted(1, 0, "serve", 0))
	fibers.Observe(background, fiberStarted(2, 1, "detached", time.Millisecond))
	fibers.Observe(background, fiberCompleted(1, 0, 2*time.Millisecond))

	tree := fibers.Tree()
	if len(tree) != 1 || tree[0].ID != 2 {
		t.Fatalf("expected the outliving fiber as a root, got %v", tree)
	}
	// The parent it named is kept, because it is true and says where the work
	// came from.
	if tree[0].ParentID != 1 {
		t.Fatalf("expected the forker it named kept, got %d", tree[0].ParentID)
	}
}

func TestAFibersAgeIsHowLongItHasBeenRunning(t *testing.T) {
	fibers := trace.NewFibers()
	fibers.Observe(context.Background(), fiberStarted(1, 0, "serve", 0))

	now := at(4 * time.Minute)
	tree := fibers.Tree()
	if age := tree[0].Age(now); age != 4*time.Minute {
		t.Fatalf("expected the age since it was forked, got %v", age)
	}
	// Rendered age first, which is the question a fiber view answers.
	const expected = "#1 running 4m0s, forked in serve\n"
	if rendered := fibers.Render(now); rendered != expected {
		t.Fatalf("unexpected rendering: %q", rendered)
	}
}

func TestAnOpenSpansAgeIsCountedFromNowAndAFinishedOnesIsWhatItTook(t *testing.T) {
	spans := trace.NewSpans()
	spans.Observe(context.Background(), spanStarted(1, 0, "waiting", 0))

	open := spans.Open()[0]
	if age := open.Age(at(2 * time.Minute)); age != 2*time.Minute {
		t.Fatalf("expected the age of an open span, got %v", age)
	}

	tree := trace.Assemble([]effect.RuntimeEvent{
		spanStarted(2, 0, "finished", 0),
		spanEnded(2, 0, "finished", time.Millisecond, time.Millisecond, effect.EventStatusSuccess),
	})
	if age := tree.Roots[0].Age(at(time.Hour)); age != time.Millisecond {
		t.Fatalf("expected a finished span's age to be what it took, got %v", age)
	}
}
