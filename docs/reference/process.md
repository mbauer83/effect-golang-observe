# Process reference

```go
process.Read() Reading
process.Between(before, after Reading) Change
process.Keep(capacity int) (*Series, error)
process.Accounting(names ...string) *Costs

process.Costing(costs, name, fx) effect.Effect[R, E, A]
process.Measured(costs, name, fx) effect.Effect[R, E, A]   // Costing + Named + WithSpan

func (series *Series) Sample() Reading
func (series *Series) Readings() []Reading
func (series *Series) Recent() (Change, bool)
func (change Change) Busy() float64
func (change Change) Collecting() float64
func (change Change) AllocationRate() float64
```

## Not per span, and not per fiber

Go has no per-goroutine allocation counter and no per-goroutine CPU clock.
Every number here is **process-wide**, and the names say so: a figure
attributed to one fiber would be invented rather than measured.

What is available is better than it sounds. Go reports the scheduler's own
breakdown — how many goroutines are running, runnable and waiting — which is
the nearest thing to the fiber status ZIO reports and this runtime does not
emit. **Runnable is the interesting one**: ready and not running means waiting
for a thread, so a persistent runnable count above `Threads` is a program short
of CPU rather than short of work.

`Read` is one call into `runtime/metrics`, which samples every value at one
moment — so the numbers in a `Reading` are consistent with each other, which
they would not be if each were read separately. It reads eighteen metrics and
not the full set: reading everything per refresh would make the instrument part
of what it measures.

## A window, not an instant

A gauge answers "how much now"; a `Change` answers "how fast", and the second
is the one that says whether a program is in trouble. A heap of two hundred
megabytes is a fact; two hundred megabytes allocated per second is a decision
to look at.

**`Busy` is used CPU, not elapsed time.** Go's `/cpu/classes/total` counts idle
as well as work — it is the sum of every class — so measuring against it
reported an idle program as seventy per cent busy. `Change.CPUSeconds` is total
minus idle, and `Busy` is that against the threads the program was allowed: on
four threads a second of wall-clock offers four CPU-seconds, so a program using
two of them is half busy and not twice. Both shares are clamped to one, because
the clock and the accounting are read at the same moment and measure different
things.

Counters are subtracted with a floor at zero. Two readings given in the wrong
order are a caller's mistake, and a rate of eighteen quintillion bytes per
second is a worse report of it than a zero.

## Series

`Keep` holds the last readings and forgets the rest — bounded, because this is
installed for the life of a program. It needs room for at least two: a series
of one has no change in it, and a change is what a series is for.

**Nothing samples on a schedule.** Whoever reads the series takes the sample,
so the resolution of a chart is the rate it is being looked at, and a program
nobody is watching pays nothing. A sampler would be one more goroutine to own,
and the runtime deliberately does not spawn those for a capability.

## Costs

The nearest honest thing to per-span memory and CPU: what the *process* spent
while named work ran. `AllocatedDuring` and `CPUSecondsDuring` carry the caveat
in their names, because a reader who takes them for attribution will draw the
wrong conclusion and a paragraph elsewhere will not stop them — on a busy
program, concurrent work is in these numbers.

That is still worth having. A handler that allocates ten megabytes per request
shows up the first time anybody looks, and no amount of span nesting would have
said so.

Bounded by a declared vocabulary, exactly as a
[metric label](metrics.md) is: a name per request is a series per request, and
an unlisted name is accounted under `Unnamed`.

`Costing` takes its first reading **when the effect is interpreted**, not when
it is described, so one description measured twice records two runs. The second
reading is a finalizer, so work that failed or was interrupted is accounted
too — a request that allocated a hundred megabytes and then gave up is exactly
the one worth seeing. A `nil` account makes `Costing` the effect itself, so a
caller may pass one rather than branch around it.

`Measured` is `Costing` with a name and a span: the three wanted together
whenever a stage of some work deserves its own account.

```go
effect.Gen(func(do *effect.Do[Env, Refusal]) Report {
    held := do.Await(process.Measured(costs, "read", store.All()))
    return do.Await(process.Measured(costs, "digest", digesting(held)))
})
```

## Allocation, counted and shaped

In Go the **count** is usually more actionable than the weight. An allocation
costs tens of nanoseconds and a pointer for the collector to chase whatever its
size, so a hundred kilobytes in four thousand small boxes costs far more than
the same bytes in one buffer — and only the count tells them apart.

```go
change.AllocatedObjects   // how many
change.MeanObjectBytes()  // their average size
cost.ObjectsPerRun()
cost.MeanObjectBytes()
```

`Sizing` is `Accounting` that also keeps **which sizes** those allocations
were, from Go's own allocation-size histogram. A separate constructor because
it carries more, not because it costs more to read: reading the 68-bucket
histogram measured at 311ns against 290ns for the scalars alone — twenty
nanoseconds. What a caller is choosing is the *keeping*: a set of size classes
per name.

`Spread.Banded()` gathers the 68 classes into six — ≤64B, ≤256B, ≤1KiB, ≤4KiB,
≤32KiB, larger — because a busy program touches nearly every class and the raw
list is a wall rather than a disclosure. The edges are where Go's own behaviour
changes: the tiny allocator, the size classes, and past 32KiB a large object
straight from the heap.

**Not per effect.** An effect is a description and the interpreter walks
millions of nodes; two metric reads at 290ns each per node would cost orders of
magnitude more than the work. A span is the granularity where the measurement
can be cheaper than the thing measured — and even there it often is not, which
[the web layer's own numbers](https://github.com/mbauer83/effect-golang-web/blob/main/docs/reference/web.md)
say plainly.

## The runs behind the average

```go
cost.Runs        // the recent runs of this name, newest first
run.Ended        // when the window closed
run.Change       // what the process did during it
process.KeptRuns // how many of them a name keeps
```

An account is an average over every run of a name. That is the right answer to
"which work is expensive" and the wrong one to "what did *this* span do": the
average moves while the program runs, so a trace that ended a minute ago would
keep changing its numbers — and a run that allocated ten times the usual amount
is invisible in it, which is the run worth finding.

So each name keeps its recent windows as well as their sum. They are
attributed **by time**: a run's window closes inside the span it belonged to,
so a caller holding both can say which run was which. That is the only join
available — this package holds no spans, and Go reports no per-goroutine
allocation for one to be keyed by.

Bounded like everything else here, at `KeptRuns` per name. A name that runs
twice a second outlives thirty-two runs in sixteen seconds, which was the first
number tried and was not enough for a window of traces.

## Two resolution limits worth knowing

**Go's CPU accounting does not move over a fast window.** A handler that
answers in twenty microseconds reports zero CPU, because the counters advance
in larger steps than that.

**The allocation counters lag slightly.** They do move over a window of
microseconds, which is what makes them useful here — but Go accounts
allocations per span and per processor and flushes those in batches, so a
reading taken immediately after a burst is a per cent or two behind it.
Measured at about 99% of a known 2,000 allocations. Close enough to act on, not
close enough to reconcile.

The same batching is why a **single** run of a few microseconds often reports
zero: nothing it allocated had been flushed when its window closed, and the
next run carries it. Over a name's runs that averages out; over one run it is a
zero to read as "below the counter's resolution" rather than as "allocated
nothing". A column of zeros with occasional spikes is that artefact; a rising
line is not.

**A window measures the work it wraps and nothing outside it.** Where `Costing`
wraps a web handler, the request's codecs are outside it — see the
[inspector](https://github.com/mbauer83/effect-golang-observe-web/blob/main/docs/reference/inspect.md).

## Deliberately absent

A sampler goroutine, an exposition format, and any attempt to divide a
process-wide number between the fibers that were running. The first is the
caller's to own, the second belongs in an adapter, and the third cannot be done
honestly with what Go reports.
