# Delivery reference

`observe` gets a runtime's events somewhere useful. It owns no question about
them: a span tree is [`trace`](trace.md)'s and an aggregate is
[`metrics`](metrics.md)'s. Every value here is an `effect.Observer` and
composes with any other.

```go
observe.Fanout(observers ...effect.Observer) effect.Observer
observe.Filter(observer effect.Observer, keep Predicate) effect.Observer
observe.NewBuffer(observer effect.Observer, capacity int, overflow Overflow) (*Buffer, error)
observe.NewRecent(capacity int) (*Recent, error)

observe.OfKind(kinds ...effect.EventKind) Predicate
observe.Unsuccessful() Predicate
```

## Why any of this is needed

The runtime calls an observer **inline, on the fiber that caused the event**,
and says so: an observer must return promptly, and an adapter that blocks or
exports remotely must own and document its queue, its shutdown, and what it
does when the queue is full. Those three are exactly what nobody can choose on
a caller's behalf, so they are stated here rather than decided.

## Fanout

A runtime takes one observer and a program usually wants three. `Fanout`
delivers to each in the order given, on the caller's fiber, so two observers
cannot see the same run in two different orders.

It is a `Flusher`, so a `Fanout` holding a `Buffer` is drained by
`Runtime.Close` exactly as the `Buffer` alone would be. A refusal from one
observer does not skip the others — the first is reported and the rest still
drain, because a queue nobody emptied is worse than a message nobody read.

A `nil` among the observers is ignored. Assembling observers from configuration
produces one, and delivering to it would be a defect the runtime then has to
report for a mistake this can absorb.

## Selection

`Filter` is where the cost of observing is decided. Every event crosses it on
the emitting fiber, so a predicate that rejects early is what makes an
expensive observer affordable — and rejecting here means the observer never has
to know it is being filtered.

`OfKind` is safe to build a map from because `EventKind` is a bounded
vocabulary. An operation name or an attribute value is not, which is the whole
subject of [the metrics reference](metrics.md).

## The queue

```go
buffered, err := observe.NewBuffer(exporter, 4096, observe.DropOldest)
```

Three named overflow policies rather than a flag, because "true" does not say
which of the three it meant and the choice is real:

| Policy | What a full queue does | Who wants it |
|---|---|---|
| `DropNewest` | discards the arriving event | an aggregate, where the queued events are as good as the new one |
| `DropOldest` | discards the longest-waiting event | a window on what is happening now |
| `Block` | makes the emitting fiber wait | an audit trail, where the events are the point and slowing the work is the honest price |

**`Drops()` is the only evidence a buffered pipeline lost anything**, so it is
readable rather than logged. A program that reports its own telemetry should
report this too.

**Delivery keeps the emitting context's values and drops its cancellation.** An
interrupted run is exactly when the events matter most, and its context is
cancelled by the time a queue drains; an exporter handed that context would
abandon them.

**`Flush` drains the queue and ends the queueing.** A `Runtime` calls it at
`Close` for any observer that buffers, so installing one is enough to have it
drained. A second `Flush` does nothing. An owner who never installs it must
`Flush` it, or the worker outlives the program's interest in it.

An event arriving *after* `Flush` is delivered on the caller's fiber rather than
queued or dropped. The queue exists to keep observed work fast, and once it has
been drained there is no observed work left to keep fast — while there are
still events: a runtime emits `runtime_closed` **after** flushing its
capabilities, so a buffer that stopped listening at `Flush` could never deliver
the event that says the runtime closed.

## The window

`NewRecent` holds the last *n* events and forgets the rest. A run of any length
emits more events than anything wants to hold, so the question a debugger asks
is not "what happened" but "what happened just now" — and a fixed window is the
only way an observer installed for the life of a program may answer it.

`Count()` counts everything, including what has been forgotten, so a reader can
tell a quiet program from a window that has already turned over. `Events()`
returns a copy, oldest first; the window keeps being written to, and a reader
walking the live one would see events move under it.

## Deliberately absent

Adaptive sampling, batching by time, retrying a failed export, and any
particular telemetry ecosystem. Each is a policy with a real cost, and an
exporter belongs in a module that only people using that ecosystem install —
which is what the runtime's own
[integration boundary](https://github.com/mbauer83/effect-golang/blob/main/docs/reference/observability.md)
says.
