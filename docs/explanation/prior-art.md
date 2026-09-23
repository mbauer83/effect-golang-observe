# What this takes from ZIO and Effect

Both ecosystems have solved this before, and neither solution transfers whole:
one runs on the JVM with a supervisor inside the runtime, the other ships a
protocol to an editor extension. What transfers is the set of questions they
found worth answering, and the shapes they found for the answers.

## From ZIO

**A fiber dump is age first.** `Fiber.Dump` is `(fiberId, status, stackTrace)`,
and `FiberRenderer` prints the fiber's lifetime and what it is waiting on
before anything else — `"worker" #432 (16m2s) waiting on fiber #283`. That
ordering is the insight: the number that identifies a stuck program is how long
something has been running, not what it is. `Fibers.Render` prints age first
for the same reason, and `Age` exists on both `Span` and `Fiber` because
`Duration` cannot answer it — the runtime measures work when it *ends*.

**Fibers are a tree, walked from the roots.** `Fiber.dumpAllWith` starts at
`Fiber.roots` and recurses through each fiber's children. `Fibers.Tree`
does the same from `ParentFiber`, and takes the same view of an orphan: a
fiber whose forker is gone is a root, not a hole.

**Two things of ZIO's are missing, and cannot be added from here.** A
`Fiber.Status` distinguishes `Running` from `Suspended(blockingOn)`, and a dump
carries a stack trace. Neither is derivable from the events this runtime emits;
both would have to come from the runtime. Reporting "running" for a fiber that
is blocked would be worse than saying so.

**Metric state is a small closed set.** ZIO's `MetricState` is counter, gauge,
histogram, summary and frequency, and its labels are `MetricLabel` key/value
pairs — bounded by construction, which is the rule
[metrics](../reference/metrics.md) is built on.

**Self time is what a flame graph computes.** A span's duration less its
children's is the number ZIO's and Effect's profiling views rank by, and
`Span.Self` is that. It is the difference between "this route is slow" and
"this stage is slow", and only the second is actionable.

## From Effect

**The devtools wire schema is the shape worth copying.** `DevToolsSchema` sends
a span as identity, name, attributes, a `SpanStatus` of `Started(startTime)` or
`Ended(startTime, endTime, exit)`, and an optional parent. Two things follow
that this module took:

- **every span carries its start**, not only its duration. That is what a
  waterfall needs, and it is why the inspector's wire shape has an offset per
  span rather than a duration alone.
- **span status is a sum, not a flag.** A span either started or ended, and an
  ended one has an exit. `Span.IsOpen()` plus a zero `Duration` is the flattened
  form of the same statement, and `Open` is a question rather than an absence
  for that reason.

**A `SpanEvent` is a first-class message** with a name, a timestamp and
attributes. `Span.Events` is the same idea; the timestamp is what lets an event
be placed on a timeline rather than only listed under its span.

**Histogram state is buckets plus count, min, max and sum** — exactly
`Distribution`, arrived at independently and worth keeping identical, because
it is what every exposition format wants. `Frequency`, a map from string to
occurrences, is what `Snapshot.Counts` is by another name.

## What neither offers, because Go does not

Both ecosystems can attribute allocation and CPU to a fiber, because both
runtimes schedule their own fibers and can account for them. Go schedules
goroutines and exposes **no per-goroutine allocation counter and no
per-goroutine CPU clock** — so [process](../reference/process.md) measures the
process and says so, in the field names rather than in a footnote.

What Go does offer, and neither of them does, is the scheduler's own
breakdown: running, runnable and waiting goroutine counts. Runnable above the
thread count is a program short of CPU, which is a diagnosis neither a span
tree nor a fiber dump gives.

## Where this deliberately differs

**Pull in-process, not push over a protocol.** Effect's devtools are a client
and a server exchanging `Ping`/`Pong`, spans, span events and a
server-initiated `MetricsRequest` over a transport, because the tool reading
them is an editor extension outside the process. The inspector here is *inside*
the program: a mountable `web.Routes` on the same listener, holding the same
telemetry, answering a snapshot when asked. Shipping the events somewhere is
the thing that becomes unnecessary, and a protocol is what it would cost.

That trade is not free, and the cases it loses are worth naming: a program
without an HTTP surface has nothing to mount the inspector on, and a program
that has already crashed cannot be asked anything. A push protocol survives
both.

**No trace or sampling identity.** Effect's spans carry a `traceId` and a
`sampled` flag, because they federate with OpenTelemetry. This runtime's spans
carry an identity and a parent and nothing about anybody else's tracing
system, which is what keeps this module dependency-free — and an adapter that
does federate is a module of its own, which is where the runtime's own
integration boundary says it belongs.
