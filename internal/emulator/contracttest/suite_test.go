package contracttest_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/saymer-alt/keenwhite/internal/emulator"
	"github.com/saymer-alt/keenwhite/internal/emulator/contracttest"
)

// The shipped stub must pass both the universal invariants and the exact
// fail-closed stub behaviour.
func TestSuiteAgainstUnsupportedStub(t *testing.T) {
	contracttest.RunSuite(t, &emulator.UnsupportedEnvironment{})
	contracttest.RunStubSuite(t, &emulator.UnsupportedEnvironment{})
}

// The mock adapter is staging-capable, refuses isolated runs and cleans up.
// RunSuite runs LAST: its final invariant is double-cleanup, after which
// the mock (honestly) refuses further operations.
func TestSuiteAgainstMock(t *testing.T) {
	m, err := emulator.NewMockEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Cleanup(context.Background())

	staged := contracttest.StageAndVerify(t, m)
	if filepath.Base(staged) == "" {
		t.Fatalf("staged path unusable: %q", staged)
	}
	// Capabilities default to UNKNOWN — a mock that claimed SUPPORTED by
	// default would be exactly the fabricated-success failure mode.
	caps, err := m.CheckCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for c, s := range caps {
		if s != emulator.Unknown {
			t.Fatalf("capability %s must default to UNKNOWN in the mock, got %s", c, s)
		}
	}
	// Refuses isolated runs even when the test pre-answers LOOPBACK:
	// isolation can never be proven by configuration alone.
	m.SetCapability(emulator.CapLoopback, emulator.Supported)
	if _, err := m.RunIsolatedTest(context.Background(), emulator.TestRequest{
		Name: "x", StagedArtifact: staged, IsolatedNetwork: true,
	}); err == nil {
		t.Fatal("mock must refuse isolated runs regardless of capability answers")
	}

	contracttest.RunSuite(t, m)
}
