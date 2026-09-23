package process

// The runs behind an account.

import "time"

// Run is one run of a name: when its window ended, and what the process did
// during it.
//
// The account beside this says what a name costs on average, which is the
// right answer to "which work is expensive" and the wrong one to "what did
// this span do". An average moves as the program runs, so a trace somebody
// finished looking at would keep changing its numbers -- and a run that
// allocated ten times the usual amount is invisible in it, which is the run
// worth finding.
//
// Attributed by time rather than by identity: the window's end is inside the
// span's, so whoever holds both can say which run belongs to which span. The
// runtime's span identity does not reach a counter reader -- Go has no
// per-goroutine allocation counter and this package holds no spans -- and the
// clock does.
type Run struct {
	EndTime time.Time
	Change  Change
}

// MaxRuns is how many runs of each name are kept.
//
// Bounded like everything else here: the names are bounded, so this bounds the
// whole of it. The number is chosen against what it is for -- a trace a tool
// is still showing should still have its runs -- and thirty-two was not
// enough: a name that runs twice a second outlives its runs in sixteen
// seconds, and a window of traces is longer than that.
const MaxRuns = 256

// runs keeps the most recent runs of one name in a ring.
type runs struct {
	slots []Run
	next  int
	full  bool
}

func (ring *runs) add(run Run) {
	if ring.slots == nil {
		ring.slots = make([]Run, MaxRuns)
	}
	ring.slots[ring.next] = run
	ring.next = (ring.next + 1) % MaxRuns
	if ring.next == 0 {
		ring.full = true
	}
}

// recent are the kept runs, newest first.
func (ring *runs) recent() []Run {
	count := ring.next
	if ring.full {
		count = MaxRuns
	}
	latest := make([]Run, 0, count)
	for step := 1; step <= count; step++ {
		at := (ring.next - step + MaxRuns) % MaxRuns
		latest = append(latest, ring.slots[at])
	}
	return latest
}
