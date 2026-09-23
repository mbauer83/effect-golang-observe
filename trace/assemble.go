package trace

// Folding a finite collection of events into the tree they came from.

import (
	"cmp"
	"log/slog"
	"slices"

	"github.com/mbauer83/effect-golang/effect"
)

// Trace is what a collection of events did.
type Trace struct {
	// Roots are the spans nothing enclosed, oldest first.
	Roots []Span
	// Loose are the events that belong to no span in this collection: work
	// outside every WithSpan, and work whose span started before the window
	// these events came from. Kept rather than discarded, because a window
	// that begins mid-run is the normal case for a live tool and the events
	// are not less true for it.
	Loose []effect.RuntimeEvent
}

// Assemble reads the events as a tree.
//
// A pure fold: the same events always give the same tree, siblings included,
// so a rendered trace can be compared against a recorded one. Order comes
// from the runtime's own timestamps, with the span identity breaking a tie --
// two spans that started in the same instant are still two spans, and a stable
// order for them is worth more than pretending the instant distinguishes them.
//
// A span that never ended stays open rather than being dropped for being
// incomplete. A span whose end arrived without its start -- which a truncated
// window will do -- contributes its events to Loose and no half-span.
func Assemble(events []effect.RuntimeEvent) Trace {
	spans := map[uint64]*Span{}
	for _, event := range events {
		if event.Kind == effect.EventSpanStarted {
			spans[event.SpanID] = newSpan(event)
		}
	}

	tree := Trace{}
	for _, event := range events {
		switch {
		case event.Kind == effect.EventSpanStarted:
			continue
		case event.Kind == effect.EventSpanEnded:
			if span, known := spans[event.SpanID]; known {
				endSpan(span, event)
				continue
			}
			tree.Loose = append(tree.Loose, event)
		default:
			if span, known := spans[event.SpanID]; known {
				span.Events = append(span.Events, event)
				continue
			}
			tree.Loose = append(tree.Loose, event)
		}
	}

	tree.Roots = nest(spans)
	return tree
}

// newSpan is a span as its start describes it.
func newSpan(event effect.RuntimeEvent) *Span {
	return &Span{
		ID:         event.SpanID,
		ParentID:   event.ParentID,
		Name:       event.Operation,
		Source:     event.Source,
		FiberID:    event.FiberID,
		StartTime:  event.Timestamp,
		Attributes: event.Attributes,
	}
}

// endSpan records how it finished. The end's own attributes are appended,
// because the runtime supplies the metadata in force where the span ended and
// a reader that saw only the start's would miss what the work found out.
func endSpan(span *Span, event effect.RuntimeEvent) {
	span.EndTime = event.Timestamp
	span.Duration = event.Duration
	span.Status = event.Status
	span.Attributes = mergeAttributes(span.Attributes, event.Attributes)
}

// nest hangs each span under its parent and returns those with none.
//
// A span whose parent is not in this collection is a root here. That is the
// truthful reading of a window: the parent exists, this collection does not
// contain it, and inventing a placeholder for it would put a span in the tree
// that nothing observed.
func nest(spans map[uint64]*Span) []Span {
	identities := make([]uint64, 0, len(spans))
	for identity := range spans {
		identities = append(identities, identity)
	}
	slices.Sort(identities)

	roots := make([]uint64, 0, len(spans))
	for _, identity := range identities {
		span := spans[identity]
		parent, enclosed := spans[span.ParentID]
		if !enclosed || span.ParentID == span.ID {
			roots = append(roots, identity)
			continue
		}
		parent.Children = append(parent.Children, Span{ID: identity})
	}

	// The children were recorded as identities and are filled in afterwards,
	// so a parent seen before its child still gets it.
	tree := make([]Span, 0, len(roots))
	for _, identity := range roots {
		tree = append(tree, subtree(spans, identity))
	}
	slices.SortStableFunc(tree, byStart)
	return tree
}

// subtree reads one span and its descendants out of the map.
func subtree(spans map[uint64]*Span, identity uint64) Span {
	span := *spans[identity]
	children := make([]Span, 0, len(span.Children))
	for _, child := range span.Children {
		children = append(children, subtree(spans, child.ID))
	}
	slices.SortStableFunc(children, byStart)
	span.Children = children
	// Chronological, so a reader -- and the renderer, which merges these with
	// the children -- gets one sequence per span. Within a fiber the emission
	// order already is chronological; a span whose children ran on other
	// fibers is why this does not rely on that.
	slices.SortStableFunc(span.Events, func(first, second effect.RuntimeEvent) int {
		return first.Timestamp.Compare(second.Timestamp)
	})
	return span
}

func byStart(first Span, second Span) int {
	if order := first.StartTime.Compare(second.StartTime); order != 0 {
		return order
	}
	return cmp.Compare(first.ID, second.ID)
}

// mergeAttributes keeps the start's attributes and adds the end's, skipping a key
// the start already carries with the same value -- which is the ordinary case,
// since the runtime supplies the same inherited metadata at both boundaries.
func mergeAttributes(attributes []slog.Attr, more []slog.Attr) []slog.Attr {
	union := append([]slog.Attr{}, attributes...)
	for _, attribute := range more {
		if slices.ContainsFunc(attributes, func(original slog.Attr) bool {
			return original.Equal(attribute)
		}) {
			continue
		}
		union = append(union, attribute)
	}
	return union
}
