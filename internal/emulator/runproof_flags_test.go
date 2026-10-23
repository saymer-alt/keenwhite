package emulator

import (
	"strings"
	"testing"
)

// Task 16 stage F: boolean proof fields are not evidence merely because
// they are true. The KEENETIC_EMULATOR label requires the COMPLETE proof
// set anchored to a concrete run, guest image and test process; until a
// verified emulator executor supplies those, any run stays LOCAL_FAKE.

// The three boolean flags set true, with every anchor field missing or
// unanchored: still LOCAL_FAKE. A future executor cannot earn the label
// by flipping booleans alone.
func TestRunProof_BooleanFlagsAloneDoNotEarnLabel(t *testing.T) {
	booleansOnly := []RunProof{
		// All three booleans true, no guest/image anchor at all.
		{ExecutedInsideVM: true, IsolationProven: true, CleanupOK: true},
		// Anchored only by a free-form version, no id, no image digest.
		{GuestImageVersion: "5.1.8", ExecutedInsideVM: true, IsolationProven: true, CleanupOK: true},
		// Guest id without the exact image digest and vice versa.
		{GuestID: "guest-1", ExecutedInsideVM: true, IsolationProven: true, CleanupOK: true, GuestPID: 1},
		{GuestImageSHA256: strings.Repeat("a", 64), ExecutedInsideVM: true, IsolationProven: true, CleanupOK: true},
	}
	for i, p := range booleansOnly {
		if err := p.Validate(); err == nil {
			t.Fatalf("proof %d: booleans without complete anchoring must not validate", i)
		}
		if got := ResultSourceFor(p); got != SourceLocalFake {
			t.Fatalf("proof %d: unanchored run must stay LOCAL_FAKE, got %s", i, got)
		}
	}
}

// The label is anchored to the whole tuple: a complete proof with one
// anchor removed stays LOCAL_FAKE (regression companion to
// TestRunProofMissingPiecesStayLocalFake, which mutates the same fixture).
func TestRunProof_LabelRequiresFullTuple(t *testing.T) {
	p := RunProof{
		GuestID:           "keenos-518-guest-17",
		GuestImageVersion: "5.1.8-r2",
		GuestImageSHA256:  strings.Repeat("a", 64),
		ExecutedInsideVM:  true,
		ExitCode:          0,
		GuestPID:          4210,
		CleanupOK:         true,
		IsolationProven:   true,
	}
	if got := ResultSourceFor(p); got != SourceKeeneticEmulator {
		t.Fatalf("complete tuple must earn the label, got %s", got)
	}
	// Drop ONLY the in-VM confirmation: booleans alone cannot carry it.
	p.ExecutedInsideVM = false
	if got := ResultSourceFor(p); got != SourceLocalFake {
		t.Fatalf("dropping in-VM confirmation must stay LOCAL_FAKE, got %s", got)
	}
}
