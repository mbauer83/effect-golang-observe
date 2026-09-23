package process

// What happened between two readings.

import "time"

// Change is the difference between two readings: what was spent over a window
// rather than what is held at an instant.
//
// A gauge answers "how much now" and a change answers "how fast", and the
// second is the one that says whether a program is in trouble: a heap of two
// hundred megabytes is a fact, and two hundred megabytes allocated per second
// is a decision to look at.
type Change struct {
	// Duration is the wall-clock time the window covered, and EndTime when the later
	// reading was taken.
	//
	// EndTime is what attributes a window to the work that ran in it: an average
	// over a name says what the name costs, and only a window with a time on
	// it says what one run of it cost.
	Duration time.Duration
	EndTime  time.Time

	// AllocatedBytes and FreedBytes are what the process allocated and freed
	// during the window, AllocatedObjects how many allocations that was, and
	// GCCycles how many collections completed in it.
	AllocatedBytes   uint64
	AllocatedObjects uint64
	FreedBytes       uint64
	GCCycles         uint64

	// CPUSeconds is the CPU time the process actually used during the window,
	// summed over threads, and GCCPUSeconds the part of it the collector took.
	//
	// Used, not elapsed. Go's /cpu/classes/total counts idle time as well as
	// work -- it is the sum of every class, idle included -- so measuring
	// against it reported an idle program as seventy per cent busy. This is
	// total minus idle.
	CPUSeconds   float64
	GCCPUSeconds float64
	// Threads is how many goroutines could have been running at once, which
	// is what CPUSeconds is measured against.
	Threads uint64
}

// Diff is the change from one reading to a later one.
//
// The cumulative counters are subtracted with a floor at zero rather than
// allowed to wrap: two readings given in the wrong order are a caller's
// mistake, and a rate of eighteen quintillion bytes per second is a worse
// report of it than a zero.
func Diff(before Reading, after Reading) Change {
	return Change{
		Duration:         after.Time.Sub(before.Time),
		EndTime:          after.Time,
		AllocatedBytes:   delta(before.AllocatedBytes, after.AllocatedBytes),
		AllocatedObjects: delta(before.AllocatedObjects, after.AllocatedObjects),
		FreedBytes:       delta(before.FreedBytes, after.FreedBytes),
		GCCycles:         delta(before.GCCycles, after.GCCycles),
		CPUSeconds:       deltaSeconds(workSeconds(before), workSeconds(after)),
		GCCPUSeconds:     deltaSeconds(before.GCCPUSeconds, after.GCCPUSeconds),
		Threads:          after.Threads,
	}
}

// Busy is the share of the available CPU the process used, from zero to one.
//
// Against the threads it was allowed rather than against wall-clock time: on
// four threads, a second of wall-clock offers four seconds of CPU, and a
// program using two of them is half busy and not twice.
//
// Clamped to one. The accounting and the clock are read at the same moment but
// measure different things, and a program that used a shade more CPU than the
// window seemed to allow should report "fully busy" rather than a hundred and
// four per cent.
func (change Change) Busy() float64 {
	available := change.Duration.Seconds() * float64(max(change.Threads, 1))
	if available <= 0 {
		return 0
	}
	return min(change.CPUSeconds/available, 1)
}

// GCShare is the share of the CPU the process used that the garbage
// collector took, from zero to one.
//
// The number that says whether a program is spending its time on its own
// work: a program at ten per cent collecting is ordinary and one at sixty is
// allocating faster than it can afford.
func (change Change) GCShare() float64 {
	if change.CPUSeconds <= 0 {
		return 0
	}
	return min(change.GCCPUSeconds/change.CPUSeconds, 1)
}

// MeanObjectBytes is the average size of the allocations in the window.
//
// The number that says which kind of allocation problem this is: a few large
// buffers or a great many small boxes. The same bytes with a mean of forty
// and a mean of forty thousand call for entirely different work.
func (change Change) MeanObjectBytes() uint64 {
	if change.AllocatedObjects == 0 {
		return 0
	}
	return change.AllocatedBytes / change.AllocatedObjects
}

// AllocationRate is bytes allocated per second over the window.
func (change Change) AllocationRate() float64 {
	if change.Duration <= 0 {
		return 0
	}
	return float64(change.AllocatedBytes) / change.Duration.Seconds()
}

func delta(before uint64, after uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

// workSeconds is the CPU a reading accounts for as work: everything but idle.
func workSeconds(reading Reading) float64 {
	if reading.CPUSeconds <= reading.IdleCPUSeconds {
		return 0
	}
	return reading.CPUSeconds - reading.IdleCPUSeconds
}

func deltaSeconds(before float64, after float64) float64 {
	if after < before {
		return 0
	}
	return after - before
}
