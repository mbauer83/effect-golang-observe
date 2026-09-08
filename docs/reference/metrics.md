# Metrics reference

```go
metrics.Naming(operations ...string) Vocabulary
metrics.Collect(vocabulary Vocabulary, bounds ...time.Duration) *Collector

func (collector *Collector) Snapshot() Snapshot
func (snapshot Snapshot) Labels() []Label
func (snapshot Snapshot) Total(kind effect.EventKind) uint64
func (snapshot Snapshot) Unsuccessful(kind effect.EventKind) uint64
```

## The rule this package exists to keep

The runtime states it: **never label a measurement by a message, a path, an
error string or an arbitrary annotation value.** An unbounded label is an
unbounded map, and a metrics pipeline that grows one has stopped being
monitoring and become the incident.

So the labels here are bounded by construction, and there is no way to ask for
one that is not:

- `EventKind` and `EventStatus` are bounded by the runtime — seventeen kinds,
  five statuses.
- the operation name is bounded by the caller, who declares with `Naming` which
  of its operations are worth their own series. Everything else is measured
  under `metrics.Other`.

One bucket for everything unnamed rather than a bucket per name: the point of
declaring a vocabulary is that the number of series cannot grow with the
traffic, and an escape hatch that grew would give the guarantee away.

`Naming()` with nothing named is a legitimate choice. Every measurement is then
labelled by kind and status alone, which is the cheapest useful aggregate and
where a program with no opinion should start.

The memory is fixed before the program runs: `(names + 1) × kinds × statuses`
labels, each holding two accumulating distributions with a fixed number of
buckets. Nothing about the traffic can grow it.

## What is measured

`Counts` for every event. `Durations` where the event reported one — a span, a
fiber, a scope close. `Delays` where it reported one — a scheduled retry or
repetition.

**Only what an event carries is measured.** A `span_started` has no duration,
and recording a zero for it would put a measurement in the histogram that
nothing measured — after which every quantile is a lie about how fast the work
was.

## Distributions

`Count`, `Sum`, `Min` and `Max` are exact. The buckets are cumulative, as a
histogram's are in every exposition format worth exporting to, so the last one
holds every measurement and a quantile is read by walking them. Keeping the
measurements themselves would be exact and unbounded, which is the one thing a
long-lived aggregate may not be.

A measurement past the last bound is in `Count` and in no bucket. That is how a
reader tells "everything was fast" from "these bounds are too narrow for this".

`Quantile(share)` is the bound at or below which that share of measurements
fell — **an upper bound, not an interpolation**, because a bucketed histogram
knows bounds and does not know values. Its rank is rounded up: the median of
three measurements is the second one.

`DefaultBounds` runs from a hundred microseconds to ten seconds. Much of what a
runtime brackets is in-process and takes microseconds — a span around a pure
computation, a scope holding one value — so bounds starting at a millisecond
would put every measurement in the first bucket and answer every quantile with
the same number.

## Snapshots

`Snapshot` is a copy: the collector keeps accumulating, and a reader walking its
live maps would be reading them as they change. Taking one is what an
exporter's scrape or a GUI's refresh does. `Labels()` returns them in a stable
order, so a rendered snapshot is comparable against a recorded one.

## Deliberately absent

No exporter, and no exposition format. The runtime defines an event vocabulary
and not a telemetry backend precisely so that a program is observable without
acquiring anybody's client library, and an aggregate that spoke Prometheus or
OpenTelemetry would undo that for every caller who does not use it. A `Snapshot`
is a plain value with bounded labels; an adapter for one ecosystem is a small
module that only people in that ecosystem install.
