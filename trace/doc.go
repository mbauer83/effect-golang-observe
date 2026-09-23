// Package trace reads a runtime's events as the tree they came from.
//
// The runtime emits events flat, in the order they happen, each carrying the
// span and fiber it happened in. That is the right shape to emit -- it costs
// nothing and loses nothing -- and the wrong shape to read: the question a
// person asks is "what did this do, and what did it do inside that", which is
// a tree.
//
// Two questions, two shapes, because they are bounded differently.
//
// Assemble answers "what happened" over a finite collection of events: one
// run, one request, a window somebody kept. It is a pure fold, so the same
// events always give the same tree, down to the order of siblings.
//
// Spans answers "what is happening" as an Observer. It keeps only the spans
// that are open, which is bounded by what a program is actually doing rather
// than by how long it has been doing it -- so it can be installed for the life
// of a process, which is exactly when the question gets asked.
package trace
