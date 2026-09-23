package unit

// The buckets, and the bounds they resolve.
//
// Its own file because it is a different claim from the labels': those are
// about what a measurement is called, and these are about what a bucketed
// histogram can honestly say about where the measurements fell.

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang/effect"
)

func TestBucketsAreCumulativeAndSayWhenTheBoundsAreTooNarrow(t *testing.T) {
	collector := metrics.NewCollector(metrics.NewVocabulary(),
		time.Millisecond, 10*time.Millisecond)
	for _, took := range []time.Duration{
		500 * time.Microsecond, 5 * time.Millisecond, time.Second,
	} {
		collector.Observe(context.Background(), effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Duration: took,
		})
	}

	distribution := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Unnamed,
	}]
	if distribution.Count != 3 {
		t.Fatalf("expected all three counted, got %d", distribution.Count)
	}
	if distribution.Buckets[0].Count != 1 || distribution.Buckets[1].Count != 2 {
		t.Fatalf("expected cumulative buckets, got %+v", distribution.Buckets)
	}
	// The measurement past the last bound is in Count and in no bucket, which
	// is how a reader tells "everything was fast" from "these bounds are too
	// narrow for this".
	if distribution.Buckets[1].Count == distribution.Count {
		t.Error("expected the widest bucket to exclude what exceeded it")
	}
	if distribution.Max != time.Second || distribution.Min != 500*time.Microsecond {
		t.Fatalf("unexpected extremes: %+v", distribution)
	}
	if mean := distribution.Mean(); mean != (time.Second+5500*time.Microsecond)/3 {
		t.Fatalf("unexpected mean: %v", mean)
	}
	// A quantile is the bound at or below which the share fell -- an upper
	// bound, because a bucketed histogram knows bounds and not values.
	if quantile := distribution.Quantile(0.5); quantile != 10*time.Millisecond {
		t.Fatalf("unexpected median bound: %v", quantile)
	}
}

func TestBoundsAreOrderedAndDeduplicatedWhateverTheCallerGave(t *testing.T) {
	collector := metrics.NewCollector(metrics.NewVocabulary(),
		10*time.Millisecond, time.Millisecond, 10*time.Millisecond)
	collector.Observe(context.Background(), effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Duration: 5 * time.Millisecond,
	})

	distribution := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Unnamed,
	}]
	if len(distribution.Buckets) != 2 {
		t.Fatalf("expected the repeat collapsed, got %+v", distribution.Buckets)
	}
	if distribution.Buckets[0].AtMost != time.Millisecond {
		t.Fatalf("expected ascending bounds, got %+v", distribution.Buckets)
	}
	if distribution.Buckets[0].Count != 0 || distribution.Buckets[1].Count != 1 {
		t.Fatalf("expected the measurement in the wider bucket only, got %+v", distribution.Buckets)
	}
}

func TestAQuantileIsNeverCoarserThanTheLongestMeasurement(t *testing.T) {
	// The report that read as a contradiction: a bucket bounded at a hundred
	// microseconds holding a two-microsecond measurement gave "median at most
	// 100µs" beside "longest 2µs". Both were true of the bound and the pair
	// was nonsense, and Max is the tighter bound because every measurement is
	// at or below it.
	collector := metrics.NewCollector(metrics.NewVocabulary(), 100*time.Microsecond, time.Millisecond)
	for _, took := range []time.Duration{2 * time.Microsecond, 3 * time.Microsecond} {
		collector.Observe(context.Background(), effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Duration: took,
		})
	}

	distribution := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Unnamed,
	}]
	if distribution.Max != 3*time.Microsecond {
		t.Fatalf("expected the longest measurement, got %v", distribution.Max)
	}
	if median := distribution.Quantile(0.5); median > distribution.Max {
		t.Fatalf("expected a bound no coarser than the longest, got %v against %v",
			median, distribution.Max)
	}
	// And the widest share is bounded the same way rather than by the widest
	// bucket, which nothing measured.
	if all := distribution.Quantile(1); all != distribution.Max {
		t.Fatalf("expected every measurement bounded by the longest, got %v", all)
	}
	// A bound tighter than Max is still reported as the bound: clamping must
	// not throw away resolution the buckets do have.
	collector.Observe(context.Background(), effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Duration: 900 * time.Microsecond,
	})
	wider := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Unnamed,
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
	collector := metrics.NewCollector(metrics.NewVocabulary())
	for _, took := range []time.Duration{
		2 * time.Microsecond, 3 * time.Microsecond, 40 * time.Microsecond,
	} {
		collector.Observe(context.Background(), effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Duration: took,
		})
	}

	distribution := collector.Snapshot().Durations[metrics.Label{
		Kind: effect.EventSpanEnded, Operation: metrics.Unnamed,
	}]
	if first, second := distribution.Quantile(0.5), distribution.Quantile(1); first == second {
		t.Fatalf("expected the buckets to tell these apart, got %v for both", first)
	}
	if median := distribution.Quantile(0.5); median > 10*time.Microsecond {
		t.Fatalf("expected microsecond resolution, got %v", median)
	}
}

func TestAnEventThatNamesNoOperationIsNotSweptIntoOther(t *testing.T) {
	// The two are different facts. Other means "an operation this was not
	// told to distinguish"; a runtime closing names nothing at all, and a
	// reader who cannot tell them apart goes looking for work that does not
	// exist.
	collector := metrics.NewCollector(metrics.NewVocabulary("declared"))
	background := context.Background()

	collector.Observe(background, effect.RuntimeEvent{Kind: effect.EventRuntimeClosing})
	collector.Observe(background, effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Operation: "undeclared",
	})
	collector.Observe(background, effect.RuntimeEvent{
		Kind: effect.EventSpanEnded, Operation: "declared",
	})

	under := map[string]uint64{}
	snapshot := collector.Snapshot()
	for label, count := range snapshot.Counts {
		under[label.Operation] += count
	}
	if under[metrics.Unnamed] != 1 {
		t.Fatalf("expected the unnamed event under Unnamed, got %v", under)
	}
	if under[metrics.Other] != 1 {
		t.Fatalf("expected the undeclared operation under Other, got %v", under)
	}
	if under["declared"] != 1 {
		t.Fatalf("expected the declared operation under its own name, got %v", under)
	}
	// Still bounded: three buckets for three kinds of name, and no more
	// however many undeclared names arrive.
	for index := range 200 {
		collector.Observe(background, effect.RuntimeEvent{
			Kind: effect.EventSpanEnded, Operation: "request-" + strconv.Itoa(index),
		})
	}
	if grown := len(collector.Snapshot().Labels()); grown != len(snapshot.Labels()) {
		t.Fatalf("expected the labels not to grow, got %d against %d",
			grown, len(snapshot.Labels()))
	}
}

// What the bounds are for: the error a quantile can carry.
//
// A bound is honest -- every measurement did fall at or below it -- and an
// honest answer five times the truth is not worth reading. One, two and five
// to a decade bound the overstatement at two and a half times, which is what
// makes a per-endpoint table worth looking at.
func TestNoBoundOverstatesAMeasurementByMoreThanTwoAndAHalf(t *testing.T) {
	previous := time.Duration(0)
	for _, bound := range metrics.DefaultBoundaries {
		if previous > 0 && bound > previous*5/2 {
			t.Fatalf("a measurement just over %s is reported at %s, which is %.1f times it",
				previous, bound, float64(bound)/float64(previous))
		}
		previous = bound
	}
}

// The ladder reaches far enough that a quantile is a bound rather than "at
// least the largest bucket": work waiting on somebody else's service does
// take tens of seconds.
func TestTheBoundsReachWorkThatWaitsOnSomebodyElse(t *testing.T) {
	longest := metrics.DefaultBoundaries[len(metrics.DefaultBoundaries)-1]

	if longest < time.Minute {
		t.Fatalf("the largest bound is %s, so anything slower has no bound at all", longest)
	}
	if first := metrics.DefaultBoundaries[0]; first > time.Microsecond {
		t.Fatalf("the smallest bound is %s, so every in-process span shares one bucket", first)
	}
}
