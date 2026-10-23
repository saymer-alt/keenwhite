package emulator

import (
	"errors"
	"fmt"
	"regexp"
)

// RunProof is the evidence set a test run must carry before its result may
// be labeled KEENETIC_EMULATOR (task 14 stage D3). A synthetic test that
// ran through a host mock runner is LOCAL_FAKE no matter how its callers
// label it: the label is DERIVED from this proof, never asserted by the
// caller. Every field is required; anything missing keeps LOCAL_FAKE.
type RunProof struct {
	// GuestID identifies the RUNNING guest environment instance (not the
	// image): e.g. "keenos-518-guest-17".
	GuestID string
	// GuestImageVersion is the guest image release label, e.g. "5.1.8-r2".
	GuestImageVersion string
	// GuestImageSHA256 pins the exact image bytes (64 lowercase hex).
	GuestImageSHA256 string
	// ExecutedInsideVM is the in-guest supervisor's confirmation that the
	// test process really ran inside the VM (not on the host).
	ExecutedInsideVM bool
	// ExitCode is the test binary's exit code inside the guest.
	ExitCode int
	// GuestPID is the PID of the test process observed inside the guest.
	GuestPID int
	// CleanupOK records that Cleanup terminated every child and removed
	// staged artifacts for this run.
	CleanupOK bool
	// IsolationProven records that the network-isolation checks were
	// executed and passed FOR THIS RUN (not just configured).
	IsolationProven bool
}

var (
	guestIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,63}$`)
	imageVerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{1,63}$`)
	shaHexPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Validate reports the FIRST missing piece of proof, or nil when the proof
// is complete. A complete proof still does not claim the test PASSED —
// verdicts stay in TestResult/acceptance records.
func (p RunProof) Validate() error {
	switch {
	case !guestIDPattern.MatchString(p.GuestID):
		return fmt.Errorf("run proof: running guest id required")
	case !imageVerPattern.MatchString(p.GuestImageVersion):
		return fmt.Errorf("run proof: guest image version required")
	case !shaHexPattern.MatchString(p.GuestImageSHA256):
		return fmt.Errorf("run proof: guest image SHA256 must be 64 lowercase hex")
	case !p.ExecutedInsideVM:
		return errors.New("run proof: in-VM execution must be confirmed by the in-guest supervisor")
	case !p.CleanupOK:
		return errors.New("run proof: cleanup result required")
	case !p.IsolationProven:
		return errors.New("run proof: network isolation must be proven for this run, not merely configured")
	case p.GuestPID <= 0:
		// Checked last: PID is recorded for audit; its absence alone still
		// invalidates the label, but the structural fields matter more.
		return errors.New("run proof: guest pid required")
	}
	return nil
}

// ResultSourceFor derives the honest result source for a run: only a
// COMPLETE proof earns KEENETIC_EMULATOR; everything else is LOCAL_FAKE.
// There is no third answer — an unprovable run is a host run.
func ResultSourceFor(p RunProof) ResultSource {
	if p.Validate() == nil {
		return SourceKeeneticEmulator
	}
	return SourceLocalFake
}
