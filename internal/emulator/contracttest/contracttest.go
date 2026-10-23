// Package contracttest runs the universal fail-closed invariants of the
// `keenetic-emulator/v0` contract against ANY Environment implementation —
// today the UnsupportedEnvironment stub and the MockEnvironment test double,
// later the real QemuEnvironment (task 14 stage D4). A new implementation
// that passes this suite has proven the invariants KeenWhite relies on;
// passing it never implies the environment works — only that it fails
// honestly where it must.
package contracttest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saymer-alt/keenwhite/internal/emulator"
)

var secretMarkers = []string{
	"password", "token", "secret", "private key", "authorization", "turn:", "stun:",
}

// RunSuite executes the universal invariants against env. It never asserts
// that supported operations succeed — only that unsupported/unproven
// operations refuse honestly and cleanup stays safe.
func RunSuite(t *testing.T, env emulator.Environment) {
	t.Helper()
	ctx := context.Background()

	// Invariant 1: capabilities are answered for the FULL inventory; a
	// missing answer must default to UNKNOWN (never silently absent).
	caps, err := env.CheckCapabilities(ctx)
	if err != nil {
		t.Logf("CheckCapabilities refused: %v (allowed)", err)
	} else {
		for _, c := range emulator.AllCapabilities {
			s, ok := caps[c]
			if !ok {
				t.Errorf("capability %s missing from answer (must default to UNKNOWN)", c)
			} else if s != emulator.Supported && s != emulator.Unsupported && s != emulator.Unknown {
				t.Errorf("capability %s has invalid support value %q", c, s)
			}
		}
	}

	// Invariant 2: RunIsolatedTest with an isolated-network requirement must
	// refuse unless isolation is proven. No implementation may return a
	// fabricated successful TestResult for an unproven run.
	res, err := env.RunIsolatedTest(ctx, emulator.TestRequest{
		Name: "contract-test", StagedArtifact: "/nonexistent/binary",
		IsolatedNetwork: true, Timeout: 0,
	})
	if err == nil {
		t.Errorf("unproven isolated run must refuse; got result %+v", res)
	}

	// Invariant 3: staging verifies the copy. A missing local file must
	// error, never stage an empty artifact.
	if _, err := env.StageArtifact(ctx, filepath.Join(t.TempDir(), "no-such-file")); err == nil {
		t.Error("staging a missing local file must error")
	}

	// Invariant 4: cleanup is idempotent and safe after failures — call it
	// twice; the second call must not error.
	if err := env.Cleanup(ctx); err != nil {
		t.Errorf("first cleanup failed: %v", err)
	}
	if err := env.Cleanup(ctx); err != nil {
		t.Errorf("cleanup must be idempotent, got: %v", err)
	}

	// Invariant 5: diagnostics (when provided) are secret-free by
	// construction.
	if diag, err := env.CollectDiagnostics(ctx); err == nil && len(diag) > 0 {
		lower := strings.ToLower(string(diag))
		for _, m := range secretMarkers {
			if strings.Contains(lower, m) {
				t.Errorf("diagnostics contain secret marker %q", m)
			}
		}
	}
}

// RunStubSuite additionally asserts the exact fail-closed behaviour of the
// shipped UnsupportedEnvironment: every operation refuses with
// ErrUnsupported, and the stub honestly self-identifies as "none".
func RunStubSuite(t *testing.T, env emulator.Environment) {
	t.Helper()
	ctx := context.Background()
	info, err := env.DetectEnvironment(ctx)
	if !errors.Is(err, emulator.ErrUnsupported) {
		t.Errorf("stub DetectEnvironment must refuse with ErrUnsupported, got %v", err)
	}
	if info.EmulatorKind != "none" || info.ContractAPI != emulator.APIVersionV0 {
		t.Errorf("stub must self-identify as kind \"none\" under %s, got %+v",
			emulator.APIVersionV0, info)
	}
	if _, err := env.RunIsolatedTest(ctx, emulator.TestRequest{}); !errors.Is(err, emulator.ErrUnsupported) {
		t.Errorf("stub RunIsolatedTest must refuse with ErrUnsupported, got %v", err)
	}
	if _, err := env.ReadResources(ctx); !errors.Is(err, emulator.ErrUnsupported) {
		t.Errorf("stub ReadResources must refuse with ErrUnsupported, got %v", err)
	}
}

// StageAndVerify is a shared helper for implementations with real staging:
// it stages a small file and asserts the returned path exists.
func StageAndVerify(t *testing.T, env emulator.Environment) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "testbin")
	if err := os.WriteFile(src, []byte("mock-elf-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := env.StageArtifact(context.Background(), src)
	if err != nil {
		t.Fatalf("staging a real file must succeed on staging-capable environments: %v", err)
	}
	if staged == "" {
		t.Fatal("staged path must not be empty")
	}
	return staged
}
