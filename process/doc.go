// Package process reads what the process is spending: memory and compute.
//
// Deliberately not per span and not per fiber, because Go does not offer that
// and pretending otherwise would be the worst kind of instrument. There is no
// per-goroutine allocation counter and no per-goroutine CPU clock; every
// number here is process-wide, and the names say so.
//
// What is available is better than it sounds. Go 1.25 onward reports the
// scheduler's own breakdown -- how many goroutines are running, runnable and
// waiting -- which is the nearest thing to the fiber status ZIO reports and
// this runtime does not emit: a program with forty runnable goroutines and
// four threads is a program short of CPU, and one with four hundred waiting is
// a program short of something else.
//
// A Change between two readings is where the rates come from, and it is what
// makes a window measurable: allocations during it, CPU busy during it, GC
// cycles during it. Attributing a window to the work that ran in it is the
// caller's judgement, and Costs is where that judgement is recorded -- named
// so nobody mistakes it for attribution.
package process
