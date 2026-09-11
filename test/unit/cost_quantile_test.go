package unit

// Where a name's runs fell, as opposed to what they cost on average.

import (
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/process"
)

// The figure an average hides. A name that allocates little on almost every
// run and a great deal on one in fifty has an average that describes neither
// run, and it is the rare one that wakes somebody up.
func TestAQuantileFindsTheRunAnAverageHides(t *testing.T) {
	cost := costOf(append(repeated(99, 100_000), 40_000_000)...)

	if typical := cost.AllocatedAt(0.5); typical != 100_000 {
		t.Fatalf("a typical run allocated %d", typical)
	}
	if worst := cost.AllocatedAt(1); worst != 40_000_000 {
		t.Fatalf("the worst run allocated %d", worst)
	}
	// The average is between them and describes neither.
	if average := cost.PerRun(); average <= 100_000 || average >= 40_000_000 {
		t.Fatalf("the average is %d, which is the point", average)
	}
}

// A quantile here is a measurement that happened, not a bound it fell under:
// these are the runs sorted, so every answer is one of them.
func TestEveryQuantileIsARunThatHappened(t *testing.T) {
	cost := costOf(300, 100, 200, 500, 400)

	for share, wanted := range map[float64]uint64{
		0.2: 100, 0.4: 200, 0.6: 300, 0.8: 400, 1.0: 500,
	} {
		if at := cost.AllocatedAt(share); at != wanted {
			t.Fatalf("at %v the answer was %d, wanted %d", share, at, wanted)
		}
	}
}

// A name nothing has run yet answers with nothing rather than dividing by it.
func TestANameWithNoRunsAnswersWithNothing(t *testing.T) {
	empty := process.Cost{}

	if at := empty.AllocatedAt(0.95); at != 0 {
		t.Fatalf("a name with no runs allocated %d", at)
	}
	if kept := empty.KeptRunCount(); kept != 0 {
		t.Fatalf("a name with no runs kept %d of them", kept)
	}
}

// A share outside the range answers with the largest, which is the only value
// true of every run.
func TestAShareOutsideTheRangeIsTheLargest(t *testing.T) {
	cost := costOf(10, 20, 30)

	for _, share := range []float64{0, -1, 2} {
		if at := cost.AllocatedAt(share); at != 30 {
			t.Fatalf("a share of %v answered %d", share, at)
		}
	}
}

func costOf(allocated ...uint64) process.Cost {
	runs := make([]process.Run, 0, len(allocated))
	for at, bytes := range allocated {
		runs = append(runs, process.Run{
			Ended: time.Now(),
			Change: process.Change{
				AllocatedBytes:   bytes,
				AllocatedObjects: bytes / 100,
				Over:             time.Duration(at) * time.Millisecond,
			},
		})
	}
	summed := uint64(0)
	for _, bytes := range allocated {
		summed += bytes
	}
	return process.Cost{
		Times:           uint64(len(allocated)),
		AllocatedDuring: summed,
		Runs:            runs,
	}
}

func repeated(times int, value uint64) []uint64 {
	held := make([]uint64, 0, times)
	for range times {
		held = append(held, value)
	}
	return held
}
