package unit

// Naming a trace.
//
// The runtime's span identity assembles a tree and names nothing outside one
// process, so what has to be established is that this does: the same trace
// always the same name, two traces never the same one, and the form it claims
// to be.

import (
	"regexp"
	"testing"
	"time"

	"github.com/mbauer83/effect-golang-observe/trace"
)

var uuidV8 = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-8[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestTraceIdentityIsStableForTheSameRoot(t *testing.T) {
	started := time.Date(2026, 9, 9, 12, 0, 0, 1234, time.UTC)
	root := trace.Span{ID: 7, Started: started}

	first := trace.Identity(root)
	if first != trace.Identity(root) {
		t.Fatal("expected the same root to keep its identity")
	}
	// The rest of the span is not the trace's identity: the same root read
	// again with more of it filled in is the same trace, which is what lets a
	// page keep a selection while the trace it chose is still running.
	grown := root
	grown.Name = "GET /notes"
	grown.Duration = time.Millisecond
	grown.Children = []trace.Span{{ID: 8, ParentID: 7, Started: started}}
	if trace.Identity(grown) != first {
		t.Fatal("expected an identity of the root alone")
	}
	if !uuidV8.MatchString(first) {
		t.Fatalf("expected a version 8 UUID, got %q", first)
	}
}

func TestTraceIdentityDistinguishesRootsThatShareACounter(t *testing.T) {
	started := time.Date(2026, 9, 9, 12, 0, 0, 1234, time.UTC)
	// The case a counter cannot answer: two roots numbered the same, which is
	// what a restart or a replica produces.
	same := trace.Identity(trace.Span{ID: 1, Started: started})
	later := trace.Identity(trace.Span{ID: 1, Started: started.Add(time.Nanosecond)})
	if same == later {
		t.Fatal("expected two starts to be two traces")
	}
	other := trace.Identity(trace.Span{ID: 2, Started: started})
	if same == other {
		t.Fatal("expected two roots to be two traces")
	}

	seen := map[string]bool{}
	for identity := 1; identity <= 500; identity++ {
		name := trace.Identity(trace.Span{
			ID:      uint64(identity),
			Started: started.Add(time.Duration(identity) * time.Microsecond),
		})
		if seen[name] {
			t.Fatalf("identity %q was given twice", name)
		}
		seen[name] = true
	}
}
