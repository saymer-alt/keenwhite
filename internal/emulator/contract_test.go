package emulator

// Contract tests for the keenetic-emulator/v0 stub (task #12 stage C).
// They pin the fail-closed behaviour a future REAL implementation must
// preserve: UNSUPPORTED is surfaced, never faked into a pass; isolation
// claims require proof; cleanup is idempotent; diagnostics are secret-free.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func stub() *UnsupportedEnvironment { return NewUnsupportedEnvironment() }

func TestDetectEnvironmentIsUnsupported(t *testing.T) {
	info, err := stub().DetectEnvironment(context.Background())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("detect must be unsupported, got %v", err)
	}
	if info.EmulatorKind != "none" || info.ContractAPI != APIVersionV0 {
		t.Fatalf("stub must self-identify honestly: %+v", info)
	}
}

func TestAllCapabilitiesReportedUnsupported(t *testing.T) {
	caps, err := stub().CheckCapabilities(context.Background())
	if err != nil {
		t.Fatalf("capability inventory must be answerable: %v", err)
	}
	if len(caps) != len(AllCapabilities) {
		t.Fatalf("inventory must cover every capability: %d/%d", len(caps), len(AllCapabilities))
	}
	for c, s := range caps {
		if s != Unsupported {
			t.Fatalf("stub must report UNSUPPORTED for %s, got %s", c, s)
		}
	}
	// No capability may claim network isolation without proof:
	if caps[CapIsolatedTCP] == Supported || caps[CapIsolatedUDP] == Supported {
		t.Fatal("isolation must never be claimed by the stub")
	}
}

func TestRunIsolatedTestNeverExecutes(t *testing.T) {
	_, err := stub().RunIsolatedTest(context.Background(), TestRequest{
		Name: "wdtt-smoke", StagedArtifact: "/opt/worker", IsolatedNetwork: true,
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("stub must never execute anything, got %v", err)
	}
	// Even with isolation "requested": unsupported stays unsupported.
	if _, err := stub().StageArtifact(context.Background(), "/nonexistent"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("stage must be unsupported, got %v", err)
	}
}

func TestCleanupIdempotentAndSafe(t *testing.T) {
	e := stub()
	for i := 0; i < 3; i++ {
		if err := e.Cleanup(context.Background()); err != nil {
			t.Fatalf("cleanup %d must be safe: %v", i, err)
		}
	}
}

func TestDiagnosticsSecretFreeAndHonest(t *testing.T) {
	b, err := stub().CollectDiagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("diagnostics must be JSON: %v", err)
	}
	if doc["environment"] != "unsupported" {
		t.Fatalf("diagnostics must honestly say unsupported: %s", b)
	}
	for _, banned := range []string{"token", "turn", "vk_call", "CANARY"} {
		if strings.Contains(strings.ToLower(string(b)), strings.ToLower(banned)) {
			t.Fatalf("diagnostics leak %s", banned)
		}
	}
	if strings.Contains(strings.ToLower(string(b)), "pass") {
		t.Fatal("unsupported environment must never imply a pass")
	}
}

func TestResultSourcesAreDistinct(t *testing.T) {
	set := map[ResultSource]bool{
		SourceLocalFake: true, SourceQEMUUser: true,
		SourceKeeneticEmulator: true, SourcePhysical: true,
	}
	if len(set) != 4 {
		t.Fatal("four distinct result sources required")
	}
}
