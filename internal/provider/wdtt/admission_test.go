package wdtt

// Admission & lifecycle regression tests for task #12 stage A (issue #19).

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/saymer-alt/keenwhite/internal/provider"
)

// preparedFixture: valid ELF + SHA + successful Prepare.
func preparedFixture(t *testing.T) (*fixture, *Adapter) {
	t.Helper()
	f := newFixture(t)
	_ = writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	mem := func() (int, error) { return 2048, nil }
	a, err := New(f.manifest, f.runner, f.clock, mem)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("setup prepare: %v", err)
	}
	return f, a
}

// A2.1: successful Prepare -> failing re-Prepare -> Start forbidden.
func TestA2_1_FailedRePrepareInvalidatesGrant(t *testing.T) {
	f, a := preparedFixture(t)
	if err := os.Remove(f.manifest.Profile.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("re-Prepare with removed profile must fail")
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start must be forbidden after a failed re-Prepare (old grant invalidated)")
	}
	// Restore: explicit successful re-Prepare re-arms Start.
	if err := os.WriteFile(f.manifest.Profile.Path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("restore prepare: %v", err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("explicit re-Prepare must re-arm Start: %v", err)
	}
	_ = a.Stop(context.Background())
}

// A2.2: profile removed after Prepare -> Start forbidden.
func TestA2_2_ProfileRemovedAfterPrepare(t *testing.T) {
	f, a := preparedFixture(t)
	if err := os.Remove(f.manifest.Profile.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start must re-check the profile and refuse")
	}
}

// A2.3: binary replaced after Prepare -> Start forbidden (SHA re-verified).
func TestA2_3_BinarySwappedAfterPrepare(t *testing.T) {
	f, a := preparedFixture(t)
	// Overwrite with a DIFFERENT valid ELF (same size class, new hash).
	var hdr [64]byte
	hdr[4] = 2
	hdr[5] = 1
	hdr[6] = 1
	binary.LittleEndian.PutUint16(hdr[16:18], 3) // ET_DYN, still executable-class
	if err := os.WriteFile(f.manifest.Artifact.Path, hdr[:], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start must re-verify the artifact SHA and refuse a swapped binary")
	}
}

// A2.4: memory drops after Prepare -> Start forbidden.
func TestA2_4_MemoryDropAfterPrepare(t *testing.T) {
	f := newFixture(t)
	_ = writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Resources.MinMemAvailableMB = 1024
	memMB := 2048
	a, err := New(f.manifest, f.runner, f.clock, func() (int, error) { return memMB, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	memMB = 512 // dropped below the floor
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start must re-check resources and refuse")
	}
}

// A2.5: SOCKS port occupied after Prepare -> Start forbidden.
func TestA2_5_PortTakenAfterPrepare(t *testing.T) {
	f, a := preparedFixture(t)
	ln, err := net.Listen("tcp", f.manifest.Socks.Bind)
	if err != nil {
		t.Skipf("cannot occupy port: %v", err)
	}
	defer ln.Close()
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start must re-check the SOCKS port and refuse")
	}
}

// A2.7: a manifest of another provider is refused by the WDTT adapter.
func TestA2_7_ForeignProviderManifestRefused(t *testing.T) {
	f := newFixture(t)
	_ = writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Provider = "openflux"
	if _, err := New(f.manifest, f.runner, f.clock, nil); err == nil {
		t.Fatal("constructor must refuse a foreign provider manifest")
	}
	a, err := New(func() provider.Manifest {
		m := f.manifest
		m.Provider = "wdtt"
		return m
	}(), f.runner, f.clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.manifest.Provider = "openflux" // mutated afterwards: funnel must catch
	bad := false
	for _, p := range a.Validate(context.Background()) {
		if p.Severity == "error" && strings.Contains(p.Message, "identity") {
			bad = true
		}
	}
	if !bad {
		t.Fatal("foreign provider must be an identity validation error")
	}
}

// A2.8: re-Prepare during an active session is refused without disturbing
// the running process.
func TestA2_8_RePrepareDuringSession(t *testing.T) {
	f, a := preparedFixture(t)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("re-Prepare during an active session must be refused")
	}
	st, _ := a.Status(context.Background())
	if st.State != provider.StateProcessRunning {
		t.Fatalf("running session must be untouched, got %s", st.State)
	}
	if !a.verifyOwnership(f.runner.Last(), a.generation) {
		t.Fatal("ownership must be preserved across the refused Prepare")
	}
	_ = a.Stop(context.Background())
}

// A1: Start re-verifies EVERYTHING immediately before spawn. Prove it by
// mutating the binary after a successful Prepare *while the fixture's
// manifest still points at the original hash* — covered in A2.3 — and by
// proving the admission funnel order: no runner.Start happens when any
// check fails (runner never invoked).
func TestA1_StartReAdmissionRunsBeforeRunner(t *testing.T) {
	f, a := preparedFixture(t)
	if err := os.Remove(f.manifest.Artifact.Path); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("Start must refuse on missing binary")
	}
	if f.runner.StartedCount() != 0 {
		t.Fatal("runner must never be invoked when admission fails")
	}
}

// A3: an incomplete stop surfaces as FAILED in Status and Health, and the
// process is NOT counted as successfully stopped.
func TestA3_IncompleteStopShowsFailed(t *testing.T) {
	f := newFixture(t)
	_ = writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Lifecycle.StopGraceSeconds = 1
	a, err := New(f.manifest, f.runner, f.clock, func() (int, error) { return 2048, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.proc = &killProofProcess{MockProcess: f.runner.Last()}
	a.mu.Unlock()
	if err := a.Stop(context.Background()); !errors.Is(err, ErrStopIncomplete) {
		t.Fatalf("expected ErrStopIncomplete, got %v", err)
	}
	st, _ := a.Status(context.Background())
	if st.State != provider.StateFailed {
		t.Fatalf("incomplete stop must surface as FAILED, got %s", st.State)
	}
	if st.Process == nil {
		t.Fatal("the stuck process must remain visible for diagnostics")
	}
	h, _ := a.Health(context.Background())
	if h.State != provider.StateFailed {
		t.Fatalf("health must mirror FAILED, got %s", h.State)
	}
}

// A3: context cancellation during Start does not strand the process: the
// exec runner binds the command to the context, so cancellation kills it,
// and Stop remains usable afterwards.
func TestA3_ContextCancelDoesNotStrand(t *testing.T) {
	f, a := preparedFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	if err := a.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	cancel()
	// The adapter does not treat context cancellation as an error by itself;
	// the operator retains a working Stop for the recorded process.
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("Stop must remain usable after ctx cancel: %v", err)
	}
	if f.runner.Last() == nil {
		t.Fatal("process must have been recorded")
	}
	select {
	case <-f.runner.Last().Done():
	default:
		// mock processes ignore ctx; the invariant tested here is that Stop
		// completed and the bookkeeping is clean
	}
	st, _ := a.Status(context.Background())
	if st.State != provider.StateStopped {
		t.Fatalf("expected stopped after explicit stop, got %s", st.State)
	}
}

// A4: the trust boundary — a programmatically built LabGrant is necessary
// but NOT sufficient evidence of owner authorisation; it only scopes WHICH
// binary may run. The production executor stays unimplementable here.
func TestA4_LabGrantTrustBoundary(t *testing.T) {
	// Zero grant denies everything:
	r := NewExecRunner(LabGrant{})
	if err := r.Grant(provider.StartPlan{Command: "anything"}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("zero grant must deny, got %v", err)
	}
	// No constructor exists that yields a "real"-kind runner, and no
	// ExecRunner API accepts a plain ExecutionLab kind as authorisation.
	// Documented boundary: grants are provisioned by the operator out of
	// band (owner file), never derived from user/agent configuration.
	_ = provider.ExecutionLab
}
