package unit

// What the process is spending, and the one thing worth being careful about:
// every number is process-wide, so a change is a window and not an
// attribution.

import (
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/process"
)

func TestAReadingReportsMemoryAndTheSchedulersOwnBreakdown(t *testing.T) {
	// Not a golden test -- the numbers are whatever this process is doing --
	// but the shape is assertable: a running Go program has goroutines, a
	// heap, and threads to run on.
	taken := process.Read()

	if taken.Taken.IsZero() {
		t.Fatal("expected the reading to say when it was taken")
	}
	if taken.Goroutines == 0 {
		t.Fatal("expected at least this goroutine")
	}
	if taken.Threads == 0 {
		t.Fatal("expected GOMAXPROCS to be at least one")
	}
	if taken.AllocatedBytes == 0 {
		t.Fatal("expected a Go program to have allocated something")
	}
	// The scheduler's breakdown adds up to no more than the total: they are
	// the same goroutines counted by state.
	if counted := taken.Running + taken.Runnable + taken.Waiting; counted > taken.Goroutines {
		t.Fatalf("expected the breakdown within the total, got %d of %d",
			counted, taken.Goroutines)
	}
	if taken.TotalBytes < taken.HeapBytes {
		t.Fatalf("expected the heap within the total, got %d of %d",
			taken.HeapBytes, taken.TotalBytes)
	}
}

func TestAChangeIsWhatWasSpentBetweenTwoReadings(t *testing.T) {
	before := process.Read()
	// Something that certainly allocates, so the difference is not zero for
	// want of anything happening.
	held := make([][]byte, 0, 512)
	for range 512 {
		held = append(held, make([]byte, 4096))
	}
	if len(held) != 512 {
		t.Fatal("the allocation was optimised away, which makes this test vacuous")
	}
	after := process.Read()

	change := process.Between(before, after)
	if change.AllocatedBytes == 0 {
		t.Fatal("expected the allocations to show in the window")
	}
	if change.Over <= 0 {
		t.Fatal("expected the window to have a length")
	}
	if rate := change.AllocationRate(); rate <= 0 {
		t.Fatalf("expected a rate, got %v", rate)
	}
	// Busy is measured against the threads available, so it is a share and
	// cannot exceed one however many threads were working.
	if busy := change.Busy(); busy < 0 || busy > 1 {
		t.Fatalf("expected a share of the available CPU, got %v", busy)
	}
	if collecting := change.Collecting(); collecting < 0 || collecting > 1 {
		t.Fatalf("expected a share of the used CPU, got %v", collecting)
	}
}

func TestCountersGivenBackwardsReportNothingRatherThanWrapping(t *testing.T) {
	// A rate of eighteen quintillion bytes per second is a worse report of a
	// caller's mistake than a zero.
	//
	// Stated rather than sampled: two readings taken a microsecond apart may
	// hold identical counters, and subtracting equal numbers gives zero
	// whether or not anything guards the wrap.
	before := process.Reading{
		Taken:          time.Unix(100, 0),
		AllocatedBytes: 1 << 30,
		FreedBytes:     1 << 20,
		GCCycles:       9,
		CPUSeconds:     8,
		GCCPUSeconds:   2,
	}
	after := process.Reading{Taken: time.Unix(200, 0), Threads: 4}

	change := process.Between(before, after)
	if change.AllocatedBytes != 0 || change.FreedBytes != 0 || change.GCCycles != 0 {
		t.Fatalf("expected no wrap in the whole numbers, got %+v", change)
	}
	if change.CPUSeconds != 0 || change.GCCPUSeconds != 0 {
		t.Fatalf("expected no negative CPU, got %+v", change)
	}
	// And forwards it is the plain difference, so the guard has not eaten a
	// real measurement.
	forwards := process.Between(after, before)
	if forwards.AllocatedBytes != 1<<30 || forwards.CPUSeconds != 8 {
		t.Fatalf("expected the difference, got %+v", forwards)
	}
}

func TestASeriesKeepsTheLastReadingsAndNeedsRoomForAChange(t *testing.T) {
	if _, err := process.Keep(1); err == nil {
		t.Error("expected a series with no room for a change to be refused")
	}
	series, err := process.Keep(3)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := series.Latest(); present {
		t.Error("expected nothing before anything was sampled")
	}
	if _, measurable := series.Recent(); measurable {
		t.Error("expected no change from no readings")
	}

	for range 5 {
		series.Sample()
	}
	held := series.Readings()
	if len(held) != 3 {
		t.Fatalf("expected the series' worth, got %d", len(held))
	}
	// Oldest first, so a chart reads left to right.
	for index := 1; index < len(held); index++ {
		if held[index].Taken.Before(held[index-1].Taken) {
			t.Fatal("expected the readings in the order they were taken")
		}
	}
	if taken := series.Taken(); taken != 5 {
		t.Fatalf("expected every sample counted, got %d", taken)
	}
	if _, measurable := series.Recent(); !measurable {
		t.Error("expected a change across the series")
	}
}
