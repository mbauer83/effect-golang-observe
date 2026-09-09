package process

// One reading of what the process is using.

import (
	"runtime/metrics"
	"time"
)

// Reading is one sample of the process's memory and compute.
//
// Every field is process-wide. Go has no per-goroutine allocation counter and
// no per-goroutine CPU clock, so a number attributed to one fiber would be
// invented rather than measured.
type Reading struct {
	Taken time.Time

	// HeapBytes is the memory currently held by live heap objects, and
	// HeapObjects how many of them there are.
	HeapBytes   uint64
	HeapObjects uint64
	// LiveBytes is what the last GC found live, and GoalBytes the size the
	// next collection is aiming at. A LiveBytes near GoalBytes means a
	// collection is close.
	LiveBytes uint64
	GoalBytes uint64
	// StackBytes is goroutine stacks, which grow with the number of fibers as
	// much as with what they do.
	StackBytes uint64
	// TotalBytes is everything the runtime has mapped, which is what an
	// operating system's idea of the process resembles.
	TotalBytes uint64
	// AllocatedBytes and FreedBytes are cumulative since the process started,
	// so a difference between two readings is what was allocated between them.
	// AllocatedObjects counts the allocations rather than their size.
	//
	// The count is the number that matters in Go. A hundred kilobytes in one
	// buffer and a hundred kilobytes in four thousand interface boxes are the
	// same bytes and completely different problems, and only the count tells
	// them apart.
	//
	// It moves over a window of microseconds, unlike the CPU counters, and it
	// lags a little: Go accounts allocations per span and per processor and
	// flushes those in batches, so a reading taken immediately after a burst
	// is a per cent or two behind it. Close enough to act on, not close
	// enough to reconcile.
	AllocatedBytes   uint64
	AllocatedObjects uint64
	FreedBytes       uint64

	// Goroutines is how many exist; Running, Runnable and Waiting are the
	// scheduler's own breakdown of them.
	//
	// Runnable is the interesting one: a goroutine that is ready and not
	// running is one waiting for a thread, so a persistent Runnable above
	// Threads is a program short of CPU rather than short of work.
	Goroutines uint64
	Running    uint64
	Runnable   uint64
	Waiting    uint64
	// Threads is GOMAXPROCS: how many goroutines can run at once.
	Threads uint64

	// CPUSeconds and the three below it are cumulative since the process
	// started, summed over every thread -- so on four threads a second of
	// wall-clock can add four seconds of CPU.
	CPUSeconds     float64
	UserCPUSeconds float64
	GCCPUSeconds   float64
	IdleCPUSeconds float64

	// GCCycles is how many collections have completed.
	GCCycles uint64
}

// Read takes one reading.
//
// A single call into runtime/metrics, which samples every value at one moment
// -- so the numbers in a Reading are consistent with each other, which they
// would not be if each were read separately.
func Read() Reading {
	samples := make([]metrics.Sample, len(wanted))
	for index, name := range wanted {
		samples[index].Name = name
	}
	metrics.Read(samples)

	held := map[string]metrics.Value{}
	for _, sample := range samples {
		held[sample.Name] = sample.Value
	}
	return reading(time.Now(), held)
}

// wanted is what a reading is made of, and the only metrics this reads: the
// full set is large, and reading all of it per refresh would make the
// instrument part of what it measures.
var wanted = []string{
	"/memory/classes/heap/objects:bytes",
	"/gc/heap/objects:objects",
	"/gc/heap/live:bytes",
	"/gc/heap/goal:bytes",
	"/memory/classes/heap/stacks:bytes",
	"/memory/classes/total:bytes",
	"/gc/heap/allocs:bytes",
	"/gc/heap/allocs:objects",
	"/gc/heap/frees:bytes",
	"/sched/goroutines:goroutines",
	"/sched/goroutines/running:goroutines",
	"/sched/goroutines/runnable:goroutines",
	"/sched/goroutines/waiting:goroutines",
	"/sched/gomaxprocs:threads",
	"/cpu/classes/total:cpu-seconds",
	"/cpu/classes/user:cpu-seconds",
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/idle:cpu-seconds",
	"/gc/cycles/total:gc-cycles",
}

func reading(taken time.Time, held map[string]metrics.Value) Reading {
	return Reading{
		Taken:            taken,
		HeapBytes:        whole(held, "/memory/classes/heap/objects:bytes"),
		HeapObjects:      whole(held, "/gc/heap/objects:objects"),
		LiveBytes:        whole(held, "/gc/heap/live:bytes"),
		GoalBytes:        whole(held, "/gc/heap/goal:bytes"),
		StackBytes:       whole(held, "/memory/classes/heap/stacks:bytes"),
		TotalBytes:       whole(held, "/memory/classes/total:bytes"),
		AllocatedBytes:   whole(held, "/gc/heap/allocs:bytes"),
		AllocatedObjects: whole(held, "/gc/heap/allocs:objects"),
		FreedBytes:       whole(held, "/gc/heap/frees:bytes"),
		Goroutines:       whole(held, "/sched/goroutines:goroutines"),
		Running:          whole(held, "/sched/goroutines/running:goroutines"),
		Runnable:         whole(held, "/sched/goroutines/runnable:goroutines"),
		Waiting:          whole(held, "/sched/goroutines/waiting:goroutines"),
		Threads:          whole(held, "/sched/gomaxprocs:threads"),
		CPUSeconds:       fraction(held, "/cpu/classes/total:cpu-seconds"),
		UserCPUSeconds:   fraction(held, "/cpu/classes/user:cpu-seconds"),
		GCCPUSeconds:     fraction(held, "/cpu/classes/gc/total:cpu-seconds"),
		IdleCPUSeconds:   fraction(held, "/cpu/classes/idle:cpu-seconds"),
		GCCycles:         whole(held, "/gc/cycles/total:gc-cycles"),
	}
}

// whole and fraction read one value, or zero where this Go release does not
// offer it.
//
// A metric the runtime has dropped or renamed leaves a zero rather than
// failing: an instrument that refuses to report anything because one number
// moved is worse than one reporting the rest.
func whole(held map[string]metrics.Value, name string) uint64 {
	value, present := held[name]
	if !present || value.Kind() != metrics.KindUint64 {
		return 0
	}
	return value.Uint64()
}

func fraction(held map[string]metrics.Value, name string) float64 {
	value, present := held[name]
	if !present || value.Kind() != metrics.KindFloat64 {
		return 0
	}
	return value.Float64()
}
