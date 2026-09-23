package trace

// Reading a trace: walking it, asking it questions, and printing it.

import (
	"strconv"
	"strings"

	"github.com/mbauer83/effect-golang/effect"
)

// Walk visits every span, parents before children, siblings oldest first.
//
// The depth is passed rather than recomputed because a visitor that wanted it
// would otherwise have to track the recursion itself, which is the one thing
// the walk already knows.
func (trace Trace) Walk(visit func(span Span, depth int)) {
	for _, root := range trace.Roots {
		walk(root, 0, visit)
	}
}

func walk(span Span, depth int, visit func(Span, int)) {
	visit(span, depth)
	for _, child := range span.Children {
		walk(child, depth+1, visit)
	}
}

// Open are the spans that started and were not seen to end, at any depth.
//
// The first thing to ask of a program that stopped responding, which is why it
// is a question the trace answers rather than a walk every caller writes.
func (trace Trace) Open() []Span {
	return trace.spansWhere(Span.IsOpen)
}

// Unsuccessful are the spans that ended in a typed failure, a defect or an
// interruption, at any depth.
func (trace Trace) Unsuccessful() []Span {
	return trace.spansWhere(Span.IsUnsuccessful)
}

// Spans is every span in the trace, parents before children.
func (trace Trace) Spans() []Span {
	return trace.spansWhere(func(Span) bool { return true })
}

func (trace Trace) spansWhere(keep func(Span) bool) []Span {
	found := []Span{}
	trace.Walk(func(span Span, _ int) {
		if keep(span) {
			found = append(found, span)
		}
	})
	return found
}

// Render is the trace as a stable tree of lines.
//
// Stable is the point: the same events render the same text, so a recorded
// trace is something a test can compare against. Nothing here is parsed back
// -- every question the text answers, Open, Unsuccessful, Spans and the span's own
// fields answer directly, which is the rule the runtime's own cause rendering
// follows.
func (trace Trace) Render() string {
	var text strings.Builder
	for _, root := range trace.Roots {
		renderSpan(&text, root, 0)
	}
	if len(trace.Loose) > 0 {
		text.WriteString("outside every span: ")
		text.WriteString(strconv.Itoa(len(trace.Loose)))
		text.WriteString(" event(s)\n")
	}
	return text.String()
}

func renderSpan(into *strings.Builder, span Span, depth int) {
	into.WriteString(strings.Repeat("  ", depth))
	into.WriteString(describe(span))
	into.WriteString("\n")
	renderInside(into, span, depth)
}

// renderInside writes a span's own events and its children as one sequence, in
// the order they happened.
//
// Two already-ordered sequences merged, and the merge is the point: printing
// every event and then every child put a scope that closed after its children
// ran before them, which reads as a different program from the one that ran.
func renderInside(into *strings.Builder, span Span, depth int) {
	events, children := 0, 0
	for events < len(span.Events) || children < len(span.Children) {
		if takeEvent(span, events, children) {
			into.WriteString(strings.Repeat("  ", depth+1))
			into.WriteString("- ")
			into.WriteString(describeEvent(span.Events[events]))
			into.WriteString("\n")
			events++
			continue
		}
		renderSpan(into, span.Children[children], depth+1)
		children++
	}
}

// takeEvent says whether the next thing to write is an event rather than a
// child. An event at the same instant as a child goes first, because the
// runtime emits a span's start after the event that led to it.
func takeEvent(span Span, events int, children int) bool {
	if children == len(span.Children) {
		return true
	}
	if events == len(span.Events) {
		return false
	}
	return !span.Events[events].Timestamp.After(span.Children[children].StartTime)
}

func describe(span Span) string {
	name := span.Name
	if name == "" {
		name = "(unnamed)"
	}
	if span.IsOpen() {
		return name + " open"
	}
	return name + " " + span.Duration.String() + " " + statusOf(span.Status)
}

func describeEvent(event effect.RuntimeEvent) string {
	text := string(event.Kind)
	if event.Operation != "" {
		text += " " + event.Operation
	}
	if event.Status != effect.EventStatusNone {
		text += " " + statusOf(event.Status)
	}
	if event.Attempt > 0 {
		text += " attempt " + strconv.FormatUint(event.Attempt, 10)
	}
	return text
}

// statusOf names a status, including the one the runtime spells as the empty
// string: a span that ended with no classification is a fact, and rendering it
// as nothing at all would read as a missing field.
func statusOf(status effect.EventStatus) string {
	if status == effect.EventStatusNone {
		return "unclassified"
	}
	return string(status)
}
