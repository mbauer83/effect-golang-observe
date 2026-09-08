package unit

// Accounting what named work spent.
//
// Its own file because it is a different claim from the readings': those are
// about what the process is using, and these are about attributing a window to
// the work that ran in it -- which is the part that has to be careful.

import (
	"context"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/process"
	"github.com/mbauer83/effect-golang/effect"
)

func TestCostingAccountsWorkUnderABoundedName(t *testing.T) {
	costs := process.Accounting("declared")
	operations := effect.For[effect.Unit, string]()

	allocating := effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
		held := make([]byte, 1<<20)
		return effect.ExitSuccess[string](len(held))
	})
	failing := operations.Fail[int]("refused")

	effect.Run(context.Background(), effect.Unit{},
		process.Costing(costs, "declared", allocating))
	effect.Run(context.Background(), effect.Unit{},
		process.Costing(costs, "undeclared", allocating))
	// Work that failed is accounted too: a request that allocated and then
	// gave up is exactly the one worth seeing.
	effect.Run(context.Background(), effect.Unit{},
		process.Costing(costs, "declared", failing))

	taken := costs.Snapshot()
	if len(taken) != 2 {
		t.Fatalf("expected the declared name and Unnamed, got %v", taken)
	}
	under := map[string]process.Cost{}
	for _, cost := range taken {
		under[cost.Name] = cost
	}
	if declared := under["declared"]; declared.Times != 2 {
		t.Fatalf("expected both runs accounted, got %+v", declared)
	}
	if _, present := under[process.Unnamed]; !present {
		t.Fatalf("expected the undeclared name under Unnamed, got %v", taken)
	}
	if perRun := under["declared"].PerRun(); perRun == 0 {
		t.Fatal("expected the allocation to show per run")
	}
}

func TestCostingMeasuresEachInterpretationRatherThanTheDescription(t *testing.T) {
	// The description is built once and interpreted twice, so the first
	// reading has to be taken per run. One taken when the effect was
	// described would leave the second run's window stretching back to
	// then -- and it would swallow whatever happened in between.
	//
	// Which is what this measures: sixty-four megabytes are allocated between
	// the two runs, and neither run's window may contain them.
	costs := process.Accounting("twice")
	described := process.Costing(costs, "twice",
		effect.From(func(context.Context, effect.Unit) effect.Exit[string, int] {
			held := make([]byte, 1<<16)
			return effect.ExitSuccess[string](len(held))
		}))

	effect.Run(context.Background(), effect.Unit{}, described)
	between := make([][]byte, 0, 64)
	for range 64 {
		between = append(between, make([]byte, 1<<20))
	}
	if len(between) != 64 {
		t.Fatal("the allocation was optimised away, which makes this test vacuous")
	}
	effect.Run(context.Background(), effect.Unit{}, described)

	taken := costs.Snapshot()
	if len(taken) != 1 || taken[0].Times != 2 {
		t.Fatalf("expected two runs of one name, got %v", taken)
	}
	// Well under the sixty-four megabytes allocated between the runs, so the
	// windows are the runs and not the whole span since the description.
	if allocated := taken[0].AllocatedDuring; allocated > 32<<20 {
		t.Fatalf("expected the windows to be the runs, got %d bytes accounted", allocated)
	}
}

func TestCostingWithNoAccountIsTheEffectItself(t *testing.T) {
	// So a caller may pass a nil account rather than branch around it.
	value, ok := effect.Run(context.Background(), effect.Unit{},
		process.Costing[effect.Unit, string](nil, "unaccounted",
			effect.For[effect.Unit, string]().Succeed(7))).Value()
	if !ok || value != 7 {
		t.Fatalf("expected the effect unchanged, got %v", value)
	}
}

// elapsedIsTheSeriesWindow is checked here because a chart's axis depends on
// it and an empty series has no axis.
func TestElapsedIsTheWindowASeriesCovers(t *testing.T) {
	if elapsed := process.Elapsed(nil); elapsed != 0 {
		t.Fatalf("expected no window from no readings, got %v", elapsed)
	}
	readings := []process.Reading{
		{Taken: time.Unix(0, 0)},
		{Taken: time.Unix(0, 0).Add(3 * time.Second)},
	}
	if elapsed := process.Elapsed(readings); elapsed != 3*time.Second {
		t.Fatalf("expected the span of the readings, got %v", elapsed)
	}
}

func TestBusyMeasuresWorkAndNotElapsedTime(t *testing.T) {
	// The reading that gave an idle program away as seventy per cent busy:
	// Go's /cpu/classes/total counts idle as well as work, so measuring
	// against it measured the clock.
	//
	// Ten seconds on two threads offers twenty CPU-seconds. Nineteen of the
	// twenty were idle, so the program used one and is five per cent busy --
	// not ninety-five.
	before := process.Reading{Taken: time.Unix(0, 0)}
	after := process.Reading{
		Taken:          time.Unix(10, 0),
		Threads:        2,
		CPUSeconds:     20,
		IdleCPUSeconds: 19,
		GCCPUSeconds:   0.25,
	}

	change := process.Between(before, after)
	if change.CPUSeconds != 1 {
		t.Fatalf("expected the used CPU, got %v", change.CPUSeconds)
	}
	if busy := change.Busy(); busy < 0.049 || busy > 0.051 {
		t.Fatalf("expected about a twentieth busy, got %v", busy)
	}
	// And the collector's share is of what was used, not of what elapsed.
	if collecting := change.Collecting(); collecting < 0.24 || collecting > 0.26 {
		t.Fatalf("expected a quarter of the used CPU collecting, got %v", collecting)
	}
}

func TestBusyIsAShareAndNeverMoreThanOne(t *testing.T) {
	// The clock and the CPU accounting are read at the same moment and measure
	// different things, so a program can appear to have used a shade more than
	// the window allowed. "Fully busy" is the honest report of that; a hundred
	// and four per cent is not.
	change := process.Between(
		process.Reading{Taken: time.Unix(0, 0)},
		process.Reading{
			Taken:        time.Unix(1, 0),
			Threads:      1,
			CPUSeconds:   1.4,
			GCCPUSeconds: 2,
		})

	if busy := change.Busy(); busy != 1 {
		t.Fatalf("expected a full share, got %v", busy)
	}
	if collecting := change.Collecting(); collecting != 1 {
		t.Fatalf("expected a full share, got %v", collecting)
	}
}
