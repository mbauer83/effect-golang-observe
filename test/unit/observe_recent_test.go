package unit

// The window: what it keeps, in what order, and what it admits to forgetting.

import (
	"context"
	"testing"

	"github.com/mbauer83/effect-golang-observe/observe"
)

func TestTheWindowKeepsTheLastEventsOldestFirst(t *testing.T) {
	window, err := observe.NewRecent(3)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 5 {
		window.Observe(context.Background(),
			spanStarted(uint64(index+1), 0, string(rune('a'+index)), 0))
	}

	events := window.Events()
	if len(events) != 3 {
		t.Fatalf("expected the window's worth, got %d", len(events))
	}
	if events[0].Operation != "c" || events[2].Operation != "e" {
		t.Fatalf("expected the last three in order, got %v", operationsOf(events))
	}
	// Seen counts what was forgotten too, which is how a reader tells a quiet
	// program from a window that has already turned over.
	if count := window.Count(); count != 5 {
		t.Fatalf("expected every event counted, got %d", count)
	}
}

func TestAWindowThatHasNotFilledHoldsOnlyWhatArrived(t *testing.T) {
	window, err := observe.NewRecent(4)
	if err != nil {
		t.Fatal(err)
	}
	window.Observe(context.Background(), spanStarted(1, 0, "only", 0))

	events := window.Events()
	if len(events) != 1 || events[0].Operation != "only" {
		t.Fatalf("expected the one event, got %v", operationsOf(events))
	}
}

func TestAWindowNeedsRoomForAtLeastOneEvent(t *testing.T) {
	if _, err := observe.NewRecent(0); err == nil {
		t.Error("expected a window with no room to be refused")
	}
}
