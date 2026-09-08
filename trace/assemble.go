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
	building := map[uint64]*Span{}
	for _, event := range events {
		if event.Kind == effect.EventSpanStarted {
			building[event.SpanID] = startedSpan(event)
		}
	}

	assembled := Trace{}
	for _, event := range events {
		switch {
		case event.Kind == effect.EventSpanStarted:
			continue
		case event.Kind == effect.EventSpanEnded:
			if span, known := building[event.SpanID]; known {
				endSpan(span, event)
				continue
			}
			assembled.Loose = append(assembled.Loose, event)
		default:
			if span, known := building[event.SpanID]; known {
				span.Events = append(span.Events, event)
				continue
			}
			assembled.Loose = append(assembled.Loose, event)
		}
	}

	assembled.Roots = rooted(building)
	return assembled
}

// startedSpan is a span as its start describes it.
func startedSpan(event effect.RuntimeEvent) *Span {
	return &Span{
		ID:         event.SpanID,
		ParentID:   event.ParentID,
		Name:       event.Operation,
		Source:     event.Source,
		FiberID:    event.FiberID,
		Started:    event.Timestamp,
		Attributes: event.Attributes,
	}
}

// endSpan records how it finished. The end's own attributes are appended,
// because the runtime supplies the metadata in force where the span ended and
// a reader that saw only the start's would miss what the work found out.
func endSpan(span *Span, event effect.RuntimeEvent) {
	span.Ended = event.Timestamp
	span.Duration = event.Duration
	span.Status = event.Status
	span.Attributes = appendedOnce(span.Attributes, event.Attributes)
}

// rooted hangs each span under its parent and returns those with none.
//
// A span whose parent is not in this collection is a root here. That is the
// truthful reading of a window: the parent exists, this collection does not
// contain it, and inventing a placeholder for it would put a span in the tree
// that nothing observed.
func rooted(building map[uint64]*Span) []Span {
	identities := make([]uint64, 0, len(building))
	for identity := range building {
		identities = append(identities, identity)
	}
	slices.Sort(identities)

	roots := make([]uint64, 0, len(building))
	for _, identity := range identities {
		span := building[identity]
		parent, enclosed := building[span.ParentID]
		if !enclosed || span.ParentID == span.ID {
			roots = append(roots, identity)
			continue
		}
		parent.Children = append(parent.Children, Span{ID: identity})
	}

	// The children were recorded as identities and are filled in afterwards,
	// so a parent seen before its child still gets it.
	assembled := make([]Span, 0, len(roots))
	for _, identity := range roots {
		assembled = append(assembled, filled(building, identity))
	}
	slices.SortStableFunc(assembled, byStart)
	return assembled
}

// filled reads one span and its descendants out of the map.
func filled(building map[uint64]*Span, identity uint64) Span {
	span := *building[identity]
	children := make([]Span, 0, len(span.Children))
	for _, child := range span.Children {
		children = append(children, filled(building, child.ID))
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
	if started := first.Started.Compare(second.Started); started != 0 {
		return started
	}
	return cmp.Compare(first.ID, second.ID)
}

// appendedOnce keeps the start's attributes and adds the end's, skipping a key
// the start already carries with the same value -- which is the ordinary case,
// since the runtime supplies the same inherited metadata at both boundaries.
func appendedOnce(held []slog.Attr, arriving []slog.Attr) []slog.Attr {
	appended := append([]slog.Attr{}, held...)
	for _, attribute := range arriving {
		if slices.ContainsFunc(held, func(already slog.Attr) bool {
			return already.Equal(attribute)
		}) {
			continue
		}
		appended = append(appended, attribute)
	}
	return appended
}
