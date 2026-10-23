package emulator

import (
	"strings"
	"testing"
)

func fullProof() RunProof {
	return RunProof{
		GuestID:           "keenos-518-guest-17",
		GuestImageVersion: "5.1.8-r2",
		GuestImageSHA256:  strings.Repeat("a", 64),
		ExecutedInsideVM:  true,
		ExitCode:          0,
		GuestPID:          4210,
		CleanupOK:         true,
		IsolationProven:   true,
	}
}

// Task 14 D3: the KEENETIC_EMULATOR label is earned only with the complete
// proof set; anything missing keeps the run at LOCAL_FAKE.
func TestRunProofCompleteEarnsLabel(t *testing.T) {
	p := fullProof()
	if err := p.Validate(); err != nil {
		t.Fatalf("complete proof must validate: %v", err)
	}
	if got := ResultSourceFor(p); got != SourceKeeneticEmulator {
		t.Fatalf("complete proof must label KEENETIC_EMULATOR, got %s", got)
	}
}

func TestRunProofMissingPiecesStayLocalFake(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*RunProof)
	}{
		{"no guest id", func(p *RunProof) { p.GuestID = "" }},
		{"no image version", func(p *RunProof) { p.GuestImageVersion = "" }},
		{"no image sha", func(p *RunProof) { p.GuestImageSHA256 = "abc123" }},
		{"uppercase sha", func(p *RunProof) { p.GuestImageSHA256 = strings.Repeat("A", 64) }},
		{"no in-vm confirmation", func(p *RunProof) { p.ExecutedInsideVM = false }},
		{"no cleanup", func(p *RunProof) { p.CleanupOK = false }},
		{"isolation merely configured", func(p *RunProof) { p.IsolationProven = false }},
		{"no guest pid", func(p *RunProof) { p.GuestPID = 0 }},
	}
	for _, c := range cases {
		p := fullProof()
		c.mut(&p)
		if err := p.Validate(); err == nil {
			t.Fatalf("%s: proof must be incomplete", c.name)
		}
		if got := ResultSourceFor(p); got != SourceLocalFake {
			t.Fatalf("%s: unproven run must stay LOCAL_FAKE, got %s", c.name, got)
		}
	}
}

func TestRunProofZeroValueIsLocalFake(t *testing.T) {
	if got := ResultSourceFor(RunProof{}); got != SourceLocalFake {
		t.Fatalf("zero proof must be LOCAL_FAKE, got %s", got)
	}
}
