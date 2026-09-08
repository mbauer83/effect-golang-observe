// Package observe delivers a runtime's events somewhere useful.
//
// The runtime emits events inline, on the fiber that caused them, and says so:
// an observer must return promptly, and an adapter that blocks or exports
// remotely must own and document its queue, its shutdown and what it does when
// the queue is full. That is the whole of this package. It owns none of the
// questions -- a span tree is trace's, an aggregate is metrics' -- and every
// value here is an effect.Observer that composes with any other.
//
// The composition is deliberately small: one observer becomes many, many
// become one, some events are dropped, and delivery moves off the caller's
// fiber. Nothing here samples adaptively, batches by time, or retries a
// failed export, because each of those is a policy with a real cost and none
// has been asked for yet.
package observe
