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
	collector := metrics.NewCollector(metrics.NewVocabulary("load"))
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

	snapshot := collector.Snapshot()
	labels := snapshot.Labels()
	if len(labels) != 2 {
		t.Fatalf("expected the declared name and Other, got %v", labels)
	}
	// Every measurement is still counted; only the labelling is bounded.
	if total := snapshot.Total(effect.EventSpanEnded); total != 1001 {
		t.Fatalf("expected every event counted, got %d", total)
	}
	other := metrics.Label{
		Kind:      effect.EventSpanEnded,
		Status:    effect.EventStatusSuccess,
		Operation: metrics.Other,
	}
	if count := snapshot.Counts[other]; count != 1000 {
		t.Fatalf("expected the undeclared ones under Other, got %d", count)
	}
}

func TestNamingNothingLabelsByKindAndStatusAlone(t *testing.T) {
	// The cheapest useful aggregate, and where a program with no opinion
	// should start.
	collector := metrics.NewCollector(metrics.NewVocabulary())
	collector.Observe(context.Background(), effect.RuntimeEvent{
		Kind: effect.EventFiberCompleted, Operation: "anything",
		Status: effect.EventStatusDefect,
	})

	snapshot := collector.Snapshot()
	if len(snapshot.Labels()) != 1 {
		t.Fatalf("expected one label, got %v", snapshot.Labels())
	}
	if snapshot.Labels()[0].Operation != metrics.Other {
		t.Fatalf("expected everything under Other, got %v", snapshot.Labels()[0])
	}
	if unsuccessful := snapshot.Unsuccessful(effect.EventFiberCompleted); unsuccessful != 1 {
		t.Fatalf("expected the defect counted as unsuccessful, got %d", unsuccessful)
	}
}

func TestOnlyWhatAnEventCarriesIsMeasured(t *testing.T) {
	// A started event has no duration. Recording a zero for it would put a
	// measurement in the histogram that nothing measured, and every quantile
	// would then be a lie about how fast the work was.
	collector := metrics.NewCollector(metrics.NewVocabulary("work"))
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

	snapshot := collector.Snapshot()
	started := metrics.Label{Kind: effect.EventSpanStarted, Operation: "work"}
	if _, measured := snapshot.Durations[started]; measured {
		t.Error("expected no duration measured for an event that carried none")
	}
	ended := metrics.Label{
		Kind: effect.EventSpanEnded, Status: effect.EventStatusSuccess, Operation: "work",
	}
	if distribution := snapshot.Durations[ended]; distribution.Count != 1 || distribution.Max != 4*time.Millisecond {
		t.Fatalf("unexpected duration: %+v", distribution)
	}
	retried := metrics.Label{
		Kind: effect.EventRetryScheduled, Status: effect.EventStatusFailure, Operation: "work",
	}
	if distribution := snapshot.Delays[retried]; distribution.Count != 1 || distribution.Max != 20*time.Millisecond {
		t.Fatalf("unexpected delay: %+v", distribution)
	}
}
