package metrics

// The observer that aggregates, and what it hands back.

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/mbauer83/effect-golang/effect"
)

// Collector is an observer that aggregates events into bounded measurements.
//
// Its memory is fixed before the program runs: (declared names + 1) x kinds x
// statuses labels, each holding two accumulating distributions with a fixed
// number of buckets. Nothing about the traffic can grow it.
type Collector struct {
	vocabulary Vocabulary
	boundaries []time.Duration

	mutex     sync.Mutex
	counts    map[Label]uint64
	durations map[Label]*histogram
	delays    map[Label]*histogram
}

// NewCollector makes a collector over a declared vocabulary.
//
// The bounds are the duration buckets; stating none takes DefaultBoundaries.
func NewCollector(vocabulary Vocabulary, bounds ...time.Duration) *Collector {
	return &Collector{
		vocabulary: vocabulary,
		boundaries: sortBoundaries(bounds),
		counts:     map[Label]uint64{},
		durations:  map[Label]*histogram{},
		delays:     map[Label]*histogram{},
	}
}

// Observe counts the event and measures what it carries.
//
// A duration and a delay are measured only when the event carries one: a
// started event has neither, and recording a zero for it would put a
// measurement in the histogram that nothing measured.
func (collector *Collector) Observe(_ context.Context, event effect.RuntimeEvent) {
	label := collector.vocabulary.labelFor(event)

	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	collector.counts[label]++
	if event.Duration > 0 {
		collector.histogramFor(collector.durations, label).add(event.Duration)
	}
	if event.Delay > 0 {
		collector.histogramFor(collector.delays, label).add(event.Delay)
	}
}

func (collector *Collector) histogramFor(
	histograms map[Label]*histogram,
	label Label,
) *histogram {
	histogram, known := histograms[label]
	if !known {
		histogram = newHistogram(collector.boundaries)
		histograms[label] = histogram
	}
	return histogram
}

// Snapshot is the measurements as they stand.
//
// A copy: the collector keeps accumulating, and a reader walking its live maps
// would be reading them as they change. Taking one is what an exporter's scrape
// or a GUI's refresh does.
func (collector *Collector) Snapshot() Snapshot {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()

	snapshot := Snapshot{
		Counts:    make(map[Label]uint64, len(collector.counts)),
		Durations: make(map[Label]Distribution, len(collector.durations)),
		Delays:    make(map[Label]Distribution, len(collector.delays)),
	}
	for label, count := range collector.counts {
		snapshot.Counts[label] = count
	}
	for label, histogram := range collector.durations {
		snapshot.Durations[label] = histogram.snapshot()
	}
	for label, histogram := range collector.delays {
		snapshot.Delays[label] = histogram.snapshot()
	}
	return snapshot
}

// Snapshot is one reading of the measurements.
type Snapshot struct {
	// Counts is how many events carried each label.
	Counts map[Label]uint64
	// Durations is how long the work behind each label took, where the event
	// reported a duration: a fiber, a span, a scope close.
	Durations map[Label]Distribution
	// Delays is how long each label waited, where the event reported a delay:
	// a scheduled retry or repetition.
	Delays map[Label]Distribution
}

// Labels are every label the snapshot holds, in a stable order.
func (snapshot Snapshot) Labels() []Label {
	seen := map[Label]bool{}
	for label := range snapshot.Counts {
		seen[label] = true
	}
	for label := range snapshot.Durations {
		seen[label] = true
	}
	for label := range snapshot.Delays {
		seen[label] = true
	}
	labels := make([]Label, 0, len(seen))
	for label := range seen {
		labels = append(labels, label)
	}
	slices.SortFunc(labels, Label.Compare)
	return labels
}

// Total is how many events carried the kind, whatever their status or
// operation -- the count a caller usually wants first.
func (snapshot Snapshot) Total(kind effect.EventKind) uint64 {
	total := uint64(0)
	for label, count := range snapshot.Counts {
		if label.Kind == kind {
			total += count
		}
	}
	return total
}

// Unsuccessful is how many events of the kind ended in a typed failure, a
// defect or an interruption.
func (snapshot Snapshot) Unsuccessful(kind effect.EventKind) uint64 {
	total := uint64(0)
	for label, count := range snapshot.Counts {
		if label.Kind != kind {
			continue
		}
		switch label.Status {
		case effect.EventStatusFailure, effect.EventStatusDefect,
			effect.EventStatusInterrupted:
			total += count
		}
	}
	return total
}
