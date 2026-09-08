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
direct.Run(func(bind *direct.Binder[Env, Refusal]) Report {
    held := direct.Bind(bind, process.Measured(costs, "read", store.All()))
    return direct.Bind(bind, process.Measured(costs, "digest", digesting(held)))
})
```

## Two resolution limits worth knowing

**Go's CPU accounting does not move over a fast window.** A handler that
answers in twenty microseconds reports zero CPU, because the counters advance
in larger steps than that. The allocation counters are exact per allocation and
do not have this problem.

**A window measures the work it wraps and nothing outside it.** Where `Costing`
wraps a web handler, the request's codecs are outside it — see the
[inspector](https://github.com/mbauer83/effect-golang-observe-web/blob/main/docs/reference/inspect.md).

## Deliberately absent

A sampler goroutine, an exposition format, and any attempt to divide a
process-wide number between the fibers that were running. The first is the
caller's to own, the second belongs in an adapter, and the third cannot be done
honestly with what Go reports.
