// Package telemetry is a program's own telemetry, assembled, and a program
// worth pointing it at.
//
// The three packages answer three questions and a program usually wants all
// of them: what is running now, what has it been doing, and how much of it
// has there been. A runtime takes one observer, so this is what assembling
// them looks like -- and the assembly is worth showing because the order
// matters: the window and the aggregate are behind a queue, and the live span
// tracker is not.
package telemetry

import (
	"github.com/mbauer83/effect-golang-observe/metrics"
	"github.com/mbauer83/effect-golang-observe/observe"
	"github.com/mbauer83/effect-golang-observe/trace"
	"github.com/mbauer83/effect-golang/effect"
)

// Watch is everything a program needs to answer for itself.
type Watch struct {
	// Spans is the spans open right now, and Fibers the fibers. Neither is
	// buffered: the question is what is happening at this instant, and an
	// answer waiting in a queue is the wrong answer to it.
	//
	// Two trackers because they answer different questions. A span is the
	// logical structure -- what the program said it was doing -- and a fiber
	// is the execution one. A program that has stopped responding is found
	// through the second.
	Spans  *trace.Spans
	Fibers *trace.Fibers
	// Collector is the bounded aggregate, and Window the recent events the
	// trace is folded from. Both are behind the queue, because neither is
	// asked often enough to be worth paying for on the observed fiber.
	Collector *metrics.Collector
	Window    *observe.Recent

	buffer   *observe.Buffer
	observer effect.Observer
}

// NewWatch assembles the telemetry, labelling measurements by the operations
// named and everything else as metrics.Other.
func NewWatch(window int, operations ...string) (*Watch, error) {
	recent, err := observe.NewRecent(window)
	if err != nil {
		return nil, err
	}
	watch := &Watch{
		Spans:     trace.NewSpans(),
		Fibers:    trace.NewFibers(),
		Collector: metrics.NewCollector(metrics.NewVocabulary(operations...)),
		Window:    recent,
	}
	// DropOldest, because what these two answer is what happened recently: a
	// queue under pressure should lose the events nobody is going to ask
	// about rather than the ones they will.
	watch.buffer, err = observe.NewBuffer(
		observe.Fanout(watch.Collector, watch.Window), 1024, observe.DropOldest)
	if err != nil {
		return nil, err
	}
	watch.observer = observe.Fanout(watch.Spans, watch.Fibers, watch.buffer)
	return watch, nil
}

// Observer is what to install: effect.NewRuntime(effect.WithObserver(...)).
//
// It is a Flusher, so Runtime.Close drains the queue behind it. A program that
// reads the window before closing sees only what has been delivered so far,
// which is the honest consequence of not paying for delivery inline.
func (watch *Watch) Observer() effect.Observer {
	return watch.observer
}

// Trace is what the window holds, read as the tree it came from.
func (watch *Watch) Trace() trace.Trace {
	return trace.Assemble(watch.Window.Events())
}

// Drops is how many events the queue discarded, which a program reporting
// its own telemetry should report too.
func (watch *Watch) Drops() uint64 {
	return watch.buffer.Drops()
}
