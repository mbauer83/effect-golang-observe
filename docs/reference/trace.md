# Trace reference

The runtime emits events flat, in the order they happen, each carrying the span
and fiber it happened in. That is the right shape to emit — it costs nothing and
loses nothing — and the wrong shape to read: the question a person asks is
"what did this do, and what did it do inside that", which is a tree.

```go
trace.Assemble(events []effect.RuntimeEvent) Trace
trace.Watch() *Running

func (trace Trace) Render() string
func (trace Trace) Walk(visit func(span Span, depth int))
func (trace Trace) Spans() []Span
func (trace Trace) Open() []Span
func (trace Trace) Failed() []Span

func (running *Running) Open() []Span
func (running *Running) Count() int
func (running *Running) Started() uint64
func (running *Running) Ended() uint64
```

## Two questions, bounded differently

**`Assemble` answers "what happened"** over a finite collection of events: one
run, one request, a window somebody kept with
[`observe.Keep`](observe.md#the-window). It is a pure fold, so the same events
always give the same tree, siblings included.

**`Running` answers "what is happening"** as an observer. It keeps only the
spans that are open — a span is remembered when it starts and forgotten when it
ends — so a process that opens and closes a million spans holds none of them.
That is what makes it safe to install for the life of a program, which is when
the question actually gets asked. It does not accumulate the events inside a
span; those are unbounded in a long-lived span, and the collection that does
hold them is bounded by the window instead.

## What a Span carries

`ID`, `ParentID`, `Name`, `Source`, `FiberID`, `Started`, `Ended`, `Duration`,
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

**A span that never ended stays open.** `Open()` is true, `Duration` and `Ended`
are zero, and `Failed()` is false — it has not ended in anything yet. This is
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
distinguishes them. `Running.Open()` returns spans in the order they were
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
`Failed`, `Spans` and the span's own fields, which is the rule the runtime's own
[cause rendering](https://github.com/mbauer83/effect-golang/blob/main/docs/reference/cause.md)
follows.
