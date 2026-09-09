package trace

// A trace's own name.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Identity is the trace rooted at this span, named so it can be referred to.
//
// The runtime's span identity is a counter, which is enough to assemble a tree
// and not enough to name one: it starts again at one in the next process, so
// "span 16" means a different trace in every run and in every replica. A
// person copying an identity out of a tool, or a tool holding one across a
// restart, needs a name that stays the trace's own.
//
// Derived rather than stored, and from three things: a value drawn once per
// process, the root's counter, and the instant it started. So the same trace
// always has the same identity -- a page can key a selection on it and find
// it again in the next reading -- and two traces never share one, in this
// process or any other.
//
// The form is a UUID, version 8, which is the version reserved for exactly
// this: an identifier laid out by whoever made it. It is not random, and
// saying version 4 would claim it was.
func Identity(root Span) string {
	held := make([]byte, 0, 8+8+8)
	held = binary.BigEndian.AppendUint64(held, instance)
	held = binary.BigEndian.AppendUint64(held, root.ID)
	held = binary.BigEndian.AppendUint64(held, uint64(root.Started.UnixNano()))
	sum := sha256.Sum256(held)

	var bytes [16]byte
	copy(bytes[:], sum[:16])
	bytes[6] = (bytes[6] & 0x0f) | 0x80 // version 8
	bytes[8] = (bytes[8] & 0x3f) | 0x80 // the RFC's variant
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// instance separates this process's traces from every other process's,
// including its own earlier runs -- which is what the span counter cannot do.
//
// Drawn from the system's randomness at start-up. A failure to read it is a
// defect and not something to carry on quietly from: an identity that is not
// unique is worse than no identity, because it is believed.
var instance = drawn()

func drawn() uint64 {
	held := make([]byte, 8)
	if _, err := rand.Read(held); err != nil {
		panic("trace: the system's randomness is unreadable: " + err.Error())
	}
	return binary.BigEndian.Uint64(held)
}
