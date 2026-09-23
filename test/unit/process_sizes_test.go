package unit

// The shape of the allocations, not only their weight.
//
// The distinction a byte count cannot make: a hundred kilobytes in one buffer
// and a hundred kilobytes in four thousand small boxes are the same bytes and
// completely different problems.

import (
	"context"
	"math"
	"testing"

	"github.com/mbauer83/effect-golang-observe/process"
	"github.com/mbauer83/effect-golang/effect"
)

func TestAllocationsAreCountedAsWellAsWeighed(t *testing.T) {
	before := process.Read()
	buffers := make([][]byte, 0, 2000)
	for range 2000 {
		buffers = append(buffers, make([]byte, 48))
	}
	if len(buffers) != 2000 {
		t.Fatal("the allocation was optimised away, which makes this test vacuous")
	}
	after := process.Read()

	change := process.Diff(before, after)
	// Most of them, not all: Go accounts allocations per span and per
	// processor and flushes those in batches, so a reading taken immediately
	// after a burst is a little behind it. Measured here at about 99 per
	// cent, which is close enough to act on and not close enough to assert
	// exactly.
	if change.AllocObjects < 1800 {
		t.Fatalf("expected most of the two thousand allocations, got %d",
			change.AllocObjects)
	}
	// The mean is what says which kind of problem this is. Two thousand
	// forty-eight-byte slices average well under a kilobyte.
	if mean := change.MeanObjectBytes(); mean == 0 || mean > 1024 {
		t.Fatalf("expected a small mean for many small allocations, got %d", mean)
	}
}

func TestTheSpreadSaysWhichSizesTheAllocationsWere(t *testing.T) {
	before := process.ReadSizes()
	small := make([][]byte, 0, 3000)
	for range 3000 {
		small = append(small, make([]byte, 32))
	}
	large := make([][]byte, 0, 4)
	for range 4 {
		large = append(large, make([]byte, 1<<20))
	}
	if len(small) != 3000 || len(large) != 4 {
		t.Fatal("the allocations were optimised away, which makes this test vacuous")
	}
	after := process.ReadSizes()

	spread := process.DiffSizes(before, after)
	if spread.Total < 2500 {
		t.Fatalf("expected most of the allocations counted, got %d", spread.Total)
	}
	// Only the classes that saw something: sixty-eight buckets of which four
	// are non-zero is a table nobody can read.
	for _, class := range spread.Classes {
		if class.Count == 0 {
			t.Fatalf("expected no empty classes, got %+v", class)
		}
	}
	// Ascending, so the smallest sizes read first.
	for index := 1; index < len(spread.Classes); index++ {
		if spread.Classes[index].AtMost <= spread.Classes[index-1].AtMost {
			t.Fatalf("expected the classes ascending, got %+v", spread.Classes)
		}
	}
	// The many small ones dominate the count, which is the reading that
	// matters: it is the count and not the weight that says so.
	largest, present := spread.Largest()
	if !present {
		t.Fatal("expected a largest class")
	}
	if largest.AtMost > 512 {
		t.Fatalf("expected the small class to hold the most allocations, got %+v", largest)
	}
	if share := largest.Share(spread.Total); share < 0.5 {
		t.Fatalf("expected the small class to be most of them, got %v", share)
	}
	// And the megabyte slices are in there somewhere, above every small
	// class -- the last class has no upper bound and says so.
	widest := spread.Classes[len(spread.Classes)-1]
	if widest.AtMost <= 512 && !math.IsInf(widest.AtMost, 1) {
		t.Fatalf("expected the large allocations in a wider class, got %+v", widest)
	}
}

func TestSpreadingTwoReadingsThatDoNotMatchReportsNothing(t *testing.T) {
	// A caller comparing readings from different processes, or a Go release
	// that changed its size classes: nothing rather than a subtraction of
	// unrelated buckets.
	spread := process.DiffSizes(
		process.Sizes{Boundaries: []float64{1, 2}, Counts: []uint64{1}},
		process.Sizes{Boundaries: []float64{1, 2, 3}, Counts: []uint64{1, 2}})
	if spread.Total != 0 || len(spread.Classes) != 0 {
		t.Fatalf("expected nothing from mismatched readings, got %+v", spread)
	}
	if _, present := spread.Largest(); present {
		t.Fatal("expected no largest class from nothing")
	}
}

func TestAnAccountKeepsTheSizesOnlyWhenAskedTo(t *testing.T) {
	// Reading the histogram is nearly free; keeping it is a set of classes
	// per name, and a caller that does not want the detail should not carry
	// it.
	work := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		buffers := make([][]byte, 0, 500)
		for range 500 {
			buffers = append(buffers, make([]byte, 64))
		}
		return effect.ExitSuccess[string](len(buffers))
	})

	plain := process.NewCosts("work")
	if plain.KeepsSizes() {
		t.Error("expected a plain account to keep no sizes")
	}
	effect.Run(context.Background(), effect.Unit{}, process.Track(plain, "work", work))
	if cost := plain.Snapshot()[0]; cost.Spread.Total != 0 {
		t.Fatalf("expected no spread from a plain account, got %+v", cost.Spread)
	}

	sizing := process.NewCostsWithSizes("work")
	if !sizing.KeepsSizes() {
		t.Error("expected a sizing account to say so")
	}
	effect.Run(context.Background(), effect.Unit{}, process.Track(sizing, "work", work))
	cost := sizing.Snapshot()[0]
	if cost.Spread.Total == 0 {
		t.Fatalf("expected the sizes kept, got %+v", cost.Spread)
	}
	if cost.ObjectsPerRun() == 0 {
		t.Fatalf("expected the allocations counted per run, got %+v", cost)
	}
	if cost.MeanObjectBytes() == 0 || cost.MeanObjectBytes() > 4096 {
		t.Fatalf("expected a small mean, got %d", cost.MeanObjectBytes())
	}
}

func TestBandsGatherTheClassesIntoSomethingReadable(t *testing.T) {
	// Go has sixty-eight size classes and a busy program touches nearly all
	// of them, so the raw list is a wall rather than a disclosure. Six bands
	// answer the question the detail was opened for.
	before := process.ReadSizes()
	small := make([][]byte, 0, 2000)
	for range 2000 {
		small = append(small, make([]byte, 24))
	}
	large := make([][]byte, 0, 8)
	for range 8 {
		large = append(large, make([]byte, 1<<19))
	}
	if len(small) != 2000 || len(large) != 8 {
		t.Fatal("the allocations were optimised away, which makes this test vacuous")
	}

	spread := process.DiffSizes(before, process.ReadSizes())
	coarse := spread.Bands()

	if len(coarse.Classes) > 6 {
		t.Fatalf("expected a handful of bands, got %d", len(coarse.Classes))
	}
	// Never more bands than classes, and in practice far fewer: how many
	// fewer depends on what the process happened to allocate, so the claim
	// worth making is the bound.
	if len(coarse.Classes) > len(spread.Classes) {
		t.Fatalf("expected no more bands than classes, got %d against %d",
			len(coarse.Classes), len(spread.Classes))
	}
	// Nothing is lost: the bands hold every allocation the classes did.
	total := uint64(0)
	for _, band := range coarse.Classes {
		total += band.Count
	}
	if total != spread.Total {
		t.Fatalf("expected every allocation banded, got %d of %d", total, spread.Total)
	}
	if coarse.Total != spread.Total {
		t.Fatalf("expected the total kept, got %d against %d", coarse.Total, spread.Total)
	}
	// The tiny ones dominate, and the half-megabyte ones are past the last
	// bounded band.
	largest, _ := coarse.Largest()
	if largest.AtMost > 64 {
		t.Fatalf("expected the smallest band to hold the most, got %+v", largest)
	}
	widest := coarse.Classes[len(coarse.Classes)-1]
	if !math.IsInf(widest.AtMost, 1) {
		t.Fatalf("expected the half-megabyte allocations past every band, got %+v", widest)
	}
	// Ascending, and banding nothing is nothing.
	for index := 1; index < len(coarse.Classes); index++ {
		if coarse.Classes[index].AtMost <= coarse.Classes[index-1].AtMost {
			t.Fatalf("expected the bands ascending, got %+v", coarse.Classes)
		}
	}
	if empty := (process.Spread{}).Bands(); len(empty.Classes) != 0 {
		t.Fatalf("expected nothing from nothing, got %+v", empty)
	}
}
