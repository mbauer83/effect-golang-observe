package unit

// The aggregate, and the one property that matters more than any measurement
// it produces: the number of series cannot grow with the traffic.

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang/effect"
)

func TestAnUndeclaredOperationCannotGrowTheNumberOfSeries(t *testing.T) {
	// The rule the runtime states and this exists to keep: never label by an
	// unbounded value. A thousand distinct operation names must produce the
	// same number of labels as one.
	collector := metrics.Collect(metrics.Naming("load"))
	background := context.Background()

	for index := range 1000 {
		collector.Observe(background, effect.RuntimeEvent{
			Kind:      effect.EventSpanEnded,
			Operation: "request-" + strconv.Itoa(index),
			Status:    effect.EventStatusSuccess,
			Duration:  time.Millisecond,
		})
	}
	collector.Observe(background, effect.RuntimeEvent{
		Kind:      effect.EventSpanEnded,
		Operation: "load",
		Status:    effect.EventStatusSuccess,
		Duration:  time.Millisecond,
	})

	taken := collector.Snapshot()
	labels := taken.Labels()
	if len(labels) != 2 {
		t.Fatalf("expected the declared name and Other, got %v", labels)
	}
	// Every measurement is still counted; only the labelling is bounded.
	if total := taken.Total(effect.EventSpanEnded); total != 1001 {
		t.Fatalf("expected every event counted, got %d", total)
	}
	other := metrics.Label{
		Kind:      effect.EventSpanEnded,
		Status:    effect.EventStatusSuccess,
		Operation: metrics.Other,
	}
	if counted := taken.Counts[other]; counted != 1000 {
		t.Fatalf("expected the undeclared ones under Other, got %d", counted)
	}
}

func TestNamingNothingLabelsByKindAndStatusAlone(t *testing.T) {
	// The cheapest useful aggregate, and where a program with no opinion
	// should start.
	collector := metrics.Collect(metrics.Naming())
	collector.Observe(context.Background(), effect.RuntimeEvent{
		Kind: effect.EventFiberCompleted, Operation: "anything",
		Status: effect.EventStatusDefect,
	})

	taken := collector.Snapshot()
	if len(taken.Labels()) != 1 {
		t.Fatalf("expected one label, got %v", taken.Labels())
	}
	if taken.Labels()[0].Operation != metrics.Other {
		t.Fatalf("expected everything under Other, got %v", taken.Labels()[0])
	}
	if unsuccessful := taken.Unsuccessful(effect.EventFiberCompleted); unsuccessful != 1 {
		t.Fatalf("expected the defect counted as unsuccessful, got %d", unsuccessful)
	}
}

func TestOnlyWhatAnEventCarriesIsMeasured(t *testing.T) {
	// A started event has no duration. Recording a zero for it would put a
	// measurement in the histogram that nothing measured, and every quantile
	// would then be a lie about how fast the work was.
	collector := metrics.Collect(metrics.Naming("work"))
	background := context.Background()

	collector.Observe(background, effect.RuntimeEvent{
		Kind: effect.EventSpanStarted, Operation: "work",
	})
	collector.Observe(background, effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Operation: "work",
		Status: effect.EventStatusSuccess, Duration: 4 * time.Millisecond,
	})
	collector.Observe(background, effect.RuntimeEvent{
		Kind: effect.EventRetryScheduled, Operation: "work",
		Status: effect.EventStatusFailure, Delay: 20 * time.Millisecond,
	})

	taken := collector.Snapshot()
	started := metrics.Label{Kind: effect.EventSpanStarted, Operation: "work"}
	if _, measured := taken.Durations[started]; measured {
		t.Error("expected no duration measured for an event that carried none")
	}
	ended := metrics.Label{
		Kind: effect.EventSpanEnded, Status: effect.EventStatusSuccess, Operation: "work",
	}
	if held := taken.Durations[ended]; held.Count != 1 || held.Max != 4*time.Millisecond {
		t.Fatalf("unexpected duration: %+v", held)
	}
	retried := metrics.Label{
		Kind: effect.EventRetryScheduled, Status: effect.EventStatusFailure, Operation: "work",
	}
	if held := taken.Delays[retried]; held.Count != 1 || held.Max != 20*time.Millisecond {
		t.Fatalf("unexpected delay: %+v", held)
	}
}

func TestBucketsAreCumulativeAndSayWhenTheBoundsAreTooNarrow(t *testing.T) {
	collector := metrics.Collect(metrics.Naming(),
		time.Millisecond, 10*time.Millisecond)
	for _, took := range []time.Duration{
		500 * time.Microsecond, 5 * time.Millisecond, time.Second,
	} {
		collector.Observe(context.Background(), effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Duration: took,
		})
	}

	held := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Other,
	}]
	if held.Count != 3 {
		t.Fatalf("expected all three counted, got %d", held.Count)
	}
	if held.Buckets[0].Count != 1 || held.Buckets[1].Count != 2 {
		t.Fatalf("expected cumulative buckets, got %+v", held.Buckets)
	}
	// The measurement past the last bound is in Count and in no bucket, which
	// is how a reader tells "everything was fast" from "these bounds are too
	// narrow for this".
	if held.Buckets[1].Count == held.Count {
		t.Error("expected the widest bucket to exclude what exceeded it")
	}
	if held.Max != time.Second || held.Min != 500*time.Microsecond {
		t.Fatalf("unexpected extremes: %+v", held)
	}
	if mean := held.Mean(); mean != (time.Second+5500*time.Microsecond)/3 {
		t.Fatalf("unexpected mean: %v", mean)
	}
	// A quantile is the bound at or below which the share fell -- an upper
	// bound, because a bucketed histogram knows bounds and not values.
	if quantile := held.Quantile(0.5); quantile != 10*time.Millisecond {
		t.Fatalf("unexpected median bound: %v", quantile)
	}
}

func TestBoundsAreOrderedAndDeduplicatedWhateverTheCallerGave(t *testing.T) {
	collector := metrics.Collect(metrics.Naming(),
		10*time.Millisecond, time.Millisecond, 10*time.Millisecond)
	collector.Observe(context.Background(), effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Duration: 5 * time.Millisecond,
	})

	held := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Other,
	}]
	if len(held.Buckets) != 2 {
		t.Fatalf("expected the repeat collapsed, got %+v", held.Buckets)
	}
	if held.Buckets[0].AtMost != time.Millisecond {
		t.Fatalf("expected ascending bounds, got %+v", held.Buckets)
	}
	if held.Buckets[0].Count != 0 || held.Buckets[1].Count != 1 {
		t.Fatalf("expected the measurement in the wider bucket only, got %+v", held.Buckets)
	}
}

func TestAQuantileIsNeverCoarserThanTheLongestMeasurement(t *testing.T) {
	// The report that read as a contradiction: a bucket bounded at a hundred
	// microseconds holding a two-microsecond measurement gave "median at most
	// 100µs" beside "longest 2µs". Both were true of the bound and the pair
	// was nonsense, and Max is the tighter bound because every measurement is
	// at or below it.
	collector := metrics.Collect(metrics.Naming(), 100*time.Microsecond, time.Millisecond)
	for _, took := range []time.Duration{2 * time.Microsecond, 3 * time.Microsecond} {
		collector.Observe(context.Background(), effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Duration: took,
		})
	}

	held := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Other,
	}]
	if held.Max != 3*time.Microsecond {
		t.Fatalf("expected the longest measurement, got %v", held.Max)
	}
	if median := held.Quantile(0.5); median > held.Max {
		t.Fatalf("expected a bound no coarser than the longest, got %v against %v",
			median, held.Max)
	}
	// And the widest share is bounded the same way rather than by the widest
	// bucket, which nothing measured.
	if all := held.Quantile(1); all != held.Max {
		t.Fatalf("expected every measurement bounded by the longest, got %v", all)
	}
	// A bound tighter than Max is still reported as the bound: clamping must
	// not throw away resolution the buckets do have.
	collector.Observe(context.Background(), effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Duration: 900 * time.Microsecond,
	})
	wider := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Other,
	}]
	if median := wider.Quantile(0.5); median != 100*time.Microsecond {
		t.Fatalf("expected the bucket bound where it is the tighter one, got %v", median)
	}
}

func TestTheDefaultBucketsResolveWhatARuntimeActuallyBrackets(t *testing.T) {
	// A span around a Ref read or a handler answering from memory takes
	// single-digit microseconds. Bounds that put all of those in one bucket
	// answer every quantile with the same number, which is what the first two
	// choices of default did.
	collector := metrics.Collect(metrics.Naming())
	for _, took := range []time.Duration{
		2 * time.Microsecond, 3 * time.Microsecond, 40 * time.Microsecond,
	} {
		collector.Observe(context.Background(), effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Duration: took,
		})
	}

	held := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Other,
	}]
	if first, second := held.Quantile(0.5), held.Quantile(1); first == second {
		t.Fatalf("expected the buckets to tell these apart, got %v for both", first)
	}
	if median := held.Quantile(0.5); median > 10*time.Microsecond {
		t.Fatalf("expected microsecond resolution, got %v", median)
	}
}
