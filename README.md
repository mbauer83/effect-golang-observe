# effect-golang-observe

Reading what an [effect-golang](https://github.com/mbauer83/effect-golang)
runtime is doing: the spans it opened, what happened inside them, and how much
of it there has been.

The runtime already emits the events. It emits them flat, inline, on the fiber
that caused them, and deliberately stops there — it defines an event vocabulary
and not a telemetry backend, so that a program is observable without acquiring
anybody's client library. This module is the reading, and it acquires nothing
either: the standard library and the runtime, and nothing else.

## Status

| Area | State |
|---|---|
| [Delivery: fan-out, selection, a queue with a named overflow policy](docs/reference/observe.md) | usable |
| [Traces: a finite collection of events read as a tree](docs/reference/trace.md) | usable |
| [Live spans and fibers: what is open and running now, in bounded memory](docs/reference/trace.md) | usable; no suspension status or stack trace, which the runtime does not emit |
| [Metrics: bounded counts, durations and delays](docs/reference/metrics.md) | usable |
| [Process: memory, compute, allocation counts and shapes, per-name costs](docs/reference/process.md) | usable; process-wide, because Go reports no per-goroutine allocation or CPU |
| Export adapters (OpenTelemetry, Prometheus, statsd) | absent, and [deliberately](docs/reference/metrics.md) |

A GUI over all of this is
[effect-golang-observe-web](https://github.com/mbauer83/effect-golang-observe-web).

## Layout

```text
observe/                    delivery: Fanout, Filter, NewBuffer, NewRecent
trace/                      Span, Trace, Assemble, Identity, NewSpans, Fiber, NewFibers
metrics/                    Vocabulary, Label, Distribution, NewCollector
process/                    Reading, Change, Series, Sizes, Costs, Run, Track, Measure
examples/telemetry/          a program worth watching, and the watching of it
examples/cmd/observedemo/   the example as a runnable command
test/unit/                  behaviour of the public API
test/acceptance/            the example against a real runtime
test/architecture/          the claims about this module's shape
docs/                       reference
```

The four packages have no edges between them. Each owns one question, and a
program that wants two composes them — which is what keeps a caller who wanted
a span tree from also acquiring an aggregate.

## The shortest useful thing

```go
spans := trace.NewSpans()
collector := metrics.NewCollector(metrics.NewVocabulary("checkout", "charge"))
window, _ := observe.NewRecent(1024)

buffer, _ := observe.NewBuffer(observe.Fanout(collector, window), 4096, observe.DropOldest)
runtime, _ := effect.NewRuntime(effect.WithObserver(observe.Fanout(spans, buffer)))

// ... run the program, then:
runtime.Close(ctx)                          // drains the queue
fmt.Print(trace.Assemble(window.Events()).Render())
fmt.Println(spans.Count(), "span(s) still open")
```

The live span tracker is not behind the queue and the other two are, which is
the one thing worth getting right: "what is running now" must not be answered
from a backlog, and "what has it been doing" is not asked often enough to be
worth paying for on the observed fiber.

## What it produces

`go run ./examples/cmd/observedemo` runs a program that holds a resource,
retries a supplier that refuses twice per item, and fails on one item of three:

```text
restock 48.097µs typed_failure
  - scope_opened restock
  - resource_acquired restock
  item 6.031µs success
    - retry_scheduled read-level typed_failure attempt 1
    - retry_scheduled read-level typed_failure attempt 2
    - retry_succeeded read-level success attempt 3
  item 3.879µs typed_failure
    - retry_scheduled read-level typed_failure attempt 3
    - retry_exhausted read-level typed_failure attempt 4
  - scope_closing restock
  - resource_released restock
  - scope_closed restock success
```

A span's own events and its children are one sequence, in the order they
happened. The scope closed after the items ran, so it is printed after them.

## Documentation

- [Delivery](docs/reference/observe.md) — fan-out, selection, and the queue
- [Traces](docs/reference/trace.md) — the tree, the live view, and the fibers
- [Metrics](docs/reference/metrics.md) — bounded labels, and why they must be
- [Process](docs/reference/process.md) — memory, compute, and what a window can
  honestly be attributed to
- [What this takes from ZIO and Effect](docs/explanation/prior-art.md) — and
  where it deliberately differs

## Development

`go.mod` requires the runtime by version, so what a consumer resolves is what
this module was built against. Working on both at once is a workspace's job:

```sh
cd workspace
go work init ./effect-golang ./effect-golang-observe
```

Releasing is
[RELEASING.md](https://github.com/mbauer83/effect-golang/blob/main/RELEASING.md).

## Scope

An observer runs inline and must return promptly, which is the constraint
everything here is shaped by. Adaptive sampling, time-based batching, retrying
a failed export and talking to a particular telemetry ecosystem are all absent:
each is a policy with a cost, none can be chosen on a caller's behalf, and an
exporter for somebody's ecosystem belongs in a module that only people using
that ecosystem install.
