package metrics

// What a measurement may be labelled by, and why that is all.

import (
	"cmp"
	"slices"

	"github.com/mbauer83/effect-golang/effect"
)

// Other is the operation name an unlisted operation is measured under.
//
// One bucket for everything undeclared rather than a bucket per name: the
// point of declaring a vocabulary is that the number of series cannot grow
// with the traffic, and an escape hatch that grew would give the guarantee
// away.
const Other = "other"

// Anonymous is what an event that names no operation is measured under.
//
// Distinct from Other, because "this names no operation" and "this names one I
// was not told to distinguish" are different facts, and a reader who cannot
// tell them apart will go looking for work that does not exist.
const Anonymous = ""

// Vocabulary is the set of operation names measurements may distinguish.
//
// A caller declares it, because only the caller knows which of its operations
// are worth their own series. Everything else is measured as Other, which
// keeps the count of series at (names + 1) x kinds x statuses -- a number
// fixed before the program runs.
type Vocabulary struct {
	operations map[string]bool
}

// NewVocabulary declares the operation names worth their own measurements.
//
// NewVocabulary nothing is a legitimate choice: every measurement is then labelled by
// kind and status alone, which is the cheapest useful aggregate and the one a
// program with no opinion should start from.
func NewVocabulary(operations ...string) Vocabulary {
	names := make(map[string]bool, len(operations))
	for _, operation := range operations {
		if operation != "" && operation != Other {
			names[operation] = true
		}
	}
	return Vocabulary{operations: names}
}

// Names are the declared operation names, in order.
func (vocabulary Vocabulary) Names() []string {
	names := make([]string, 0, len(vocabulary.operations))
	for operation := range vocabulary.operations {
		names = append(names, operation)
	}
	slices.Sort(names)
	return names
}

// labelFor is the bounded identity of one event's measurements.
//
// An event that names no operation is labelled with none, rather than swept
// into Other. The two are different facts and reading them as one is
// misleading: Other means "an operation this was not told to distinguish",
// and the runtime's own events -- a runtime closing, a scope opening outside
// any named work -- have nothing to distinguish. Both are one bucket each, so
// telling them apart costs nothing a bounded vocabulary was protecting.
func (vocabulary Vocabulary) labelFor(event effect.RuntimeEvent) Label {
	operation := Other
	switch {
	case event.Operation == "":
		operation = Anonymous
	case vocabulary.operations[event.Operation]:
		operation = event.Operation
	}
	return Label{Kind: event.Kind, Status: event.Status, Operation: operation}
}

// Label is a measurement's identity: bounded in all three parts.
type Label struct {
	// Kind and Status come from the runtime's own bounded vocabularies.
	Kind   effect.EventKind
	Status effect.EventStatus
	// Operation is a declared name, or Other.
	Operation string
}

// Compare orders labels, so a snapshot renders and exports the same way twice.
func (label Label) Compare(other Label) int {
	if by := cmp.Compare(label.Kind, other.Kind); by != 0 {
		return by
	}
	if by := cmp.Compare(label.Operation, other.Operation); by != 0 {
		return by
	}
	return cmp.Compare(label.Status, other.Status)
}
