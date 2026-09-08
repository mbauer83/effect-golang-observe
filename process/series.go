package process

// Readings over time, for a chart.

import (
	"sync"
	"time"
)

// Series keeps the last readings and forgets the rest.
//
// Bounded for the reason every live structure here is: this is installed for
// the life of a program, and a slice that grew per sample would be the leak
// the instrument introduced.
//
// Nothing samples it on a schedule. Whoever reads the series takes the sample
// -- a page refresh, a scrape -- so the resolution of a chart is the rate it
// is being looked at, and a program nobody is watching pays nothing. A
// sampler of its own would be one more goroutine to own, and the runtime
// deliberately does not spawn those.
type Series struct {
	mutex sync.Mutex
	held  []Reading
	next  int
	full  bool
	taken uint64
}

// Keep makes a series over the last capacity readings.
func Keep(capacity int) (*Series, error) {
	if capacity < 2 {
		// Two, not one: a series of one reading has no change in it, and a
		// change is what the series is for.
		return nil, errTooFewReadings
	}
	return &Series{held: make([]Reading, capacity)}, nil
}

// Sample takes a reading, keeps it, and returns it.
func (series *Series) Sample() Reading {
	taken := Read()
	series.Add(taken)
	return taken
}

// Add keeps a reading somebody else took, which is what lets a caller sample
// once and give it to several places.
func (series *Series) Add(reading Reading) {
	series.mutex.Lock()
	defer series.mutex.Unlock()
	series.held[series.next] = reading
	series.next++
	series.taken++
	if series.next == len(series.held) {
		series.next = 0
		series.full = true
	}
}

// Readings are what the series holds, oldest first.
func (series *Series) Readings() []Reading {
	series.mutex.Lock()
	defer series.mutex.Unlock()
	if !series.full {
		return append([]Reading{}, series.held[:series.next]...)
	}
	ordered := make([]Reading, 0, len(series.held))
	ordered = append(ordered, series.held[series.next:]...)
	return append(ordered, series.held[:series.next]...)
}

// Latest is the most recent reading, and false when none has been taken.
func (series *Series) Latest() (Reading, bool) {
	held := series.Readings()
	if len(held) == 0 {
		return Reading{}, false
	}
	return held[len(held)-1], true
}

// Recent is the change across the whole series: the oldest reading to the
// newest.
//
// False when there are fewer than two, because a change needs two readings and
// reporting a zero would say the program did nothing rather than that nobody
// has looked twice.
func (series *Series) Recent() (Change, bool) {
	held := series.Readings()
	if len(held) < 2 {
		return Change{}, false
	}
	return Between(held[0], held[len(held)-1]), true
}

// Taken is how many readings have been taken, including those forgotten.
func (series *Series) Taken() uint64 {
	series.mutex.Lock()
	defer series.mutex.Unlock()
	return series.taken
}

// errTooFewReadings reports a series with no room for a change.
var errTooFewReadings = errorString("process: a series needs room for at least two readings")

type errorString string

func (message errorString) Error() string { return string(message) }

// Elapsed is how long the series covers, which is what a chart's axis is.
func Elapsed(readings []Reading) time.Duration {
	if len(readings) < 2 {
		return 0
	}
	return readings[len(readings)-1].Taken.Sub(readings[0].Taken)
}
