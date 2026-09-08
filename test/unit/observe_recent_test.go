package unit

// The window: what it keeps, in what order, and what it admits to forgetting.

import (
	"context"
	"testing"

	"github.com/mbauer83/effect-golang-observe/observe"
)

func TestTheWindowKeepsTheLastEventsOldestFirst(t *testing.T) {
	window, err := observe.Keep(3)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 5 {
		window.Observe(context.Background(),
			spanStarted(uint64(index+1), 0, string(rune('a'+index)), 0))
	}

	held := window.Events()
	if len(held) != 3 {
		t.Fatalf("expected the window's worth, got %d", len(held))
	}
	if held[0].Operation != "c" || held[2].Operation != "e" {
		t.Fatalf("expected the last three in order, got %v", operationsOf(held))
	}
	// Seen counts what was forgotten too, which is how a reader tells a quiet
	// program from a window that has already turned over.
	if seen := window.Seen(); seen != 5 {
		t.Fatalf("expected every event counted, got %d", seen)
	}
}

func TestAWindowThatHasNotFilledHoldsOnlyWhatArrived(t *testing.T) {
	window, err := observe.Keep(4)
	if err != nil {
		t.Fatal(err)
	}
	window.Observe(context.Background(), spanStarted(1, 0, "only", 0))

	held := window.Events()
	if len(held) != 1 || held[0].Operation != "only" {
		t.Fatalf("expected the one event, got %v", operationsOf(held))
	}
}

func TestAWindowNeedsRoomForAtLeastOneEvent(t *testing.T) {
	if _, err := observe.Keep(0); err == nil {
		t.Error("expected a window with no room to be refused")
	}
}
