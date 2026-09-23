# Trace reference

The runtime emits events flat, in the order they happen, each carrying the span
and fiber it happened in. That is the right shape to emit — it costs nothing and
loses nothing — and the wrong shape to read: the question a person asks is
"what did this do, and what did it do inside that", which is a tree.

```go
trace.Assemble(events []effect.RuntimeEvent) Trace
trace.NewSpans() *Spans     // the spans open now
trace.NewFibers() *Fibers   // the fibers running now

func (trace Trace) Render() string
func (trace Trace) Walk(visit func(span Span, depth int))
func (trace Trace) Spans() []Span
func (trace Trace) Open() []Span
func (trace Trace) Unsuccessful() []Span

func (spans *Spans) Open() []Span
func (spans *Spans) Count() int
func (spans *Spans) Starts() uint64
func (spans *Spans) Ends() uint64

func (fibers *Fibers) Tree() []Fiber
func (fibers *Fibers) Render(now time.Time) string
func Identity(root Span) string
func (span Span) IsOpen() bool
func (span Span) IsUnsuccessful() bool
func (span Span) Age(now time.Time) time.Duration
func (fiber Fiber) Age(now time.Time) time.Duration
```

## Two questions, bounded differently

**`Assemble` answers "what happened"** over a finite collection of events: one
run, one request, a window somebody kept with
[`observe.NewRecent`](observe.md#the-window). It is a pure fold, so the same events
always give the same tree, siblings included.

**`Spans` answers "what is happening"** as an observer. It keeps only the
spans that are open — a span is remembered when it starts and forgotten when it
ends — so a process that opens and closes a million spans holds none of them.
That is what makes it safe to install for the life of a program, which is when
the question actually gets asked. It does not accumulate the events inside a
span; those are unbounded in a long-lived span, and the collection that does
hold them is bounded by the window instead.

## Two structures, because they answer different questions

A span is the **logical** structure: what the program said it was doing. A
fiber is the **execution** structure: what the runtime is actually running.
A program that has stopped responding is found through the second, which is
why ZIO's `Fiber.dump` reports a fiber's age before it reports anything else
about it, and why `dumpAllWith` walks the tree from the roots rather than
listing fibers flat.

`NewFibers` is that view. It keeps only the running fibers, nested as they
were forked, oldest first — by the runtime's own timestamps and not by the
order the events arrived, because fibers that begin at once arrive in whichever
order their goroutines got scheduled, and a live view that reordered itself
between two readings of the same three fibers would be unreadable.

A fiber whose forker has already completed is a root here. That is the truthful
reading rather than a hole in the tree: the parent is gone, this one is not,
and a program forking work that outlives its forker is doing something
deliberate.

**`Age` is the question a live view exists to answer.** Twelve spans open is a
program working; one span open for four minutes is a program stuck. `Duration`
cannot say it, because the runtime measures a span when it *ends*.

Two things ZIO's dump has and this cannot: a **suspension status** — whether a
fiber is running or blocked, and on what — and a **stack trace**. Neither is
derivable from the events this runtime emits, and both would have to come from
the runtime rather than from a reader of it. Saying so beats reporting
"running" for a fiber that is blocked.

## Naming a trace

```go
trace.Identity(root)  // "5f370d49-9187-81b2-bc29-a42ad8c814cc"
```

The runtime's span identity is a counter: enough to assemble a tree, and not
enough to name one. It starts again at one in the next process, so "span 16"
means a different trace in every run and in every replica — and a person
copying an identity out of a tool, or a tool holding a selection across a
restart, needs a name that stays the trace's own.

Derived rather than stored, from a value drawn once per process, the root's
counter and the instant it started. So the same root always gives the same
identity — the rest of the span is not part of it, which is what lets a tool
keep a selection while the trace it chose is still running — and two roots
never share one.

The form is a UUID, **version 8**: the version reserved for an identifier laid
out by whoever made it. It is not random, and claiming version 4 would say it
was.

## What a Span carries

`ID`, `ParentID`, `Name`, `Source`, `FiberID`, `StartTime`, `EndTime`, `Duration`,
`Status`, `Attributes`, `Children` and `Events`.

`Duration` is **the runtime's own measurement**, taken from the `span_ended`
event, and not a subtraction of two wall-clock readings.

`Events` are what happened inside this span and outside every child of it: a
resource acquired, a retry scheduled, a log delivered. The span's own start and
end are not among them — they *are* the span.

`Attributes` are the start's, plus anything the end added that the start did not
already carry with the same value. The runtime supplies the metadata in force at
each boundary, so the inherited attributes arrive twice and something the work
found out arrives only at the end.

## The untidy collections, which are the normal ones

**A span that never ended stays open.** `IsOpen()` is true, `Duration` and `EndTime`
are zero, and `IsUnsuccessful()` is false — it has not ended in anything yet. This is
the report and not a gap: a span still open when a run finished is where a hung
program is, and dropping it for being incomplete would hide exactly that.

**An end whose start is not in the collection contributes no half-span.** Its
events go to `Loose`. A window that begins mid-run does this constantly, and
inventing a span for the end would put one in the tree that nothing observed.

**A span whose parent is not in the collection is a root here**, with the
`ParentID` it named kept — true, and what somebody stitching two windows
together needs.

`Loose` also holds the events that belong to no span at all: work outside every
`WithSpan`, and the runtime's own `runtime_closing` and `runtime_closed`.

## Ordering

Siblings are ordered by the runtime's timestamps, with the span identity
breaking a tie: two spans that started in the same instant are still two spans,
and a stable order for them is worth more than pretending the instant
distinguishes them. `Spans.Open()` returns spans in the order they were
opened, so the first one listed is the one that has been running longest.

## Rendering

`Render` is a stable tree of lines, so a recorded trace is something a test can
compare against.

**A span's own events and its children are one sequence, in the order they
happened.** Printing every event and then every child put a scope that closed
*after* its children ran before them, which reads as a different program from
the one that ran.

Nothing is parsed back out of the text. Every question it answers — what is
open, what failed, how long something took — is answered directly by `Open`,
`Unsuccessful`, `Spans` and the span's own fields, which is the rule the runtime's own
[cause rendering](https://github.com/mbauer83/effect-golang/blob/main/docs/reference/cause.md)
follows.
