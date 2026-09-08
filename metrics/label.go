package metrics

// What a measurement may be labelled by, and why that is all.

import (
	"cmp"
	"slices"

	"github.com/mbauer83/effect-golang/effect"
)

// Other is the operation name an unlisted operation is measured under.
//
// One bucket for everything unnamed rather than a bucket per name: the point
// of declaring a vocabulary is that the number of series cannot grow with the
// traffic, and an escape hatch that grew would give the guarantee away.
const Other = "other"

// Vocabulary is the set of operation names measurements may distinguish.
//
// A caller declares it, because only the caller knows which of its operations
// are worth their own series. Everything else is measured as Other, which
// keeps the count of series at (names + 1) x kinds x statuses -- a number
// fixed before the program runs.
type Vocabulary struct {
	allowed map[string]bool
}

// Naming declares the operation names worth their own measurements.
//
// Naming nothing is a legitimate choice: every measurement is then labelled by
// kind and status alone, which is the cheapest useful aggregate and the one a
// program with no opinion should start from.
func Naming(operations ...string) Vocabulary {
	allowed := make(map[string]bool, len(operations))
	for _, operation := range operations {
		if operation != "" && operation != Other {
			allowed[operation] = true
		}
	}
	return Vocabulary{allowed: allowed}
}

// Names are the declared operation names, in order.
func (vocabulary Vocabulary) Names() []string {
	named := make([]string, 0, len(vocabulary.allowed))
	for operation := range vocabulary.allowed {
		named = append(named, operation)
	}
	slices.Sort(named)
	return named
}

// labelFor is the bounded identity of one event's measurements.
func (vocabulary Vocabulary) labelFor(event effect.RuntimeEvent) Label {
	operation := Other
	if vocabulary.allowed[event.Operation] {
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
