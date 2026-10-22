package wdtt

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/saymer-alt/keenwhite/internal/provider"
)

// Regression tests for task #11 (issues #19/#20). Fixture helpers live in
// adapter_test.go (newFixture, writeTestELF, sha256File) and mock.go.

// R01: Prepare without Validate must not bypass policy.
func TestR01_PrepareWithoutValidateEnforcesPolicy(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Lifecycle.HardStopSeconds = 1801 // policy violation, Validate never called
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("Prepare must enforce the WDTT policy without a prior Validate")
	}
	if a.plan.Command != "" {
		t.Fatal("refused Prepare must not leave a plan behind")
	}
}

// R02: hard-stop values other than exactly 1800 are rejected on every path.
func TestR02_HardStopOnlyExact1800(t *testing.T) {
	for _, v := range []int{0, 1799, 1801, 86400} {
		f := newFixture(t)
		writeTestELF(t, f.manifest.Artifact.Path)
		f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
		f.manifest.Lifecycle.HardStopSeconds = v
		a, _ := f.adapter()
		for _, p := range a.Validate(context.Background()) {
			if p.Severity == "error" && p.Field == "lifecycle.hard_stop_seconds" {
				// Validate rejects; Prepare must too:
				if _, err := a.Prepare(context.Background()); err == nil {
					t.Fatalf("hard_stop=%d: Prepare must refuse", v)
				}
				goto next
			}
		}
		t.Fatalf("hard_stop=%d: Validate must produce a policy error", v)
	next:
	}
}

// R03: mutating the prepared plan (or the manifest) after Prepare is refused
// at Start; the manifest itself cannot be changed without re-derivation.
func TestR03_StartRefusesTamperedPlan(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.plan.Args = []string{"-evil-flag"}
	if err := a.Start(context.Background()); !errors.Is(err, ErrPlanTampered) {
		t.Fatalf("tampered args must yield ErrPlanTampered, got %v", err)
	}
	a.plan.Args = []string{"-config", f.manifest.Profile.Path}
	a.plan.SocksBind = "127.0.0.1:9999"
	if err := a.Start(context.Background()); !errors.Is(err, ErrPlanTampered) {
		t.Fatalf("tampered bind must yield ErrPlanTampered, got %v", err)
	}
	// Manifest hard-stop mutated after Prepare -> integrity re-check catches it.
	f2 := newFixture(t)
	writeTestELF(t, f2.manifest.Artifact.Path)
	f2.manifest.Artifact.SHA256 = sha256File(t, f2.manifest.Artifact.Path)
	a2, _ := f2.adapter()
	if _, err := a2.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Mutate the ADAPTER's own manifest (a fixture copy would not be visible):
	a2.manifest.Lifecycle.HardStopSeconds = 86400
	if err := a2.Start(context.Background()); err == nil {
		t.Fatal("manifest mutation after Prepare must be caught at Start")
	} else if !errors.Is(err, ErrPlanTampered) && !strings.Contains(err.Error(), "1800") {
		t.Fatalf("unexpected refusal class: %v", err)
	}
}

// R04: the lab executor cannot launch anything without a valid grant.
func TestR04_ExecRunnerRequiresGrant(t *testing.T) {
	f := newFixture(t)
	sha := writeTestELF(t, f.manifest.Artifact.Path)
	plan := provider.StartPlan{Command: f.manifest.Artifact.Path, Executor: provider.ExecutionLab}

	// No grant at all:
	r := NewExecRunner(LabGrant{})
	if _, err := r.Start(context.Background(), plan); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("grantless exec must be denied, got %v", err)
	}
	// Wrong binary path:
	r = NewExecRunner(LabGrant{BinaryPath: "/elsewhere/worker", SHA256: sha})
	if _, err := r.Start(context.Background(), plan); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("wrong path must be denied, got %v", err)
	}
	// Wrong hash (binary swapped after grant):
	r = NewExecRunner(LabGrant{BinaryPath: f.manifest.Artifact.Path, SHA256: strings.Repeat("0", 64)})
	if _, err := r.Start(context.Background(), plan); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("hash mismatch must be denied, got %v", err)
	}
	// Correct grant passes AUTHORISATION; the synthetic 64-byte file cannot
	// actually exec, so Start fails with an OS error, never with a denial.
	r = NewExecRunner(LabGrant{BinaryPath: f.manifest.Artifact.Path, SHA256: sha})
	if _, err := r.Start(context.Background(), plan); err != nil && errors.Is(err, ErrGrantDenied) {
		t.Fatalf("valid grant must pass authorisation, got %v", err)
	} else if err == nil {
		t.Fatal("a non-executable file must not 'start' successfully")
	}
	// A "real"-kind runner stays forbidden at adapter construction and start.
	if _, err := New(f.manifest, fakeRealRunner{}, nil, nil); err != provider.ErrForbiddenByPolicy {
		t.Fatalf("real runner refused at construction, got %v", err)
	}
}

// R05: a stale watcher must not touch the next generation's process.
func TestR05_StaleWatcherSparesNewProcess(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := f.runner.Last()
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	// New generation starts with a FRESH full deadline.
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := f.runner.Last()
	if second == first {
		t.Fatal("expected a new process object for the new generation")
	}
	// Advance past the FIRST process's deadline but stay within the new one.
	f.clock.Advance(time.Duration(WDTTHardStopSeconds-10) * time.Second)
	time.Sleep(50 * time.Millisecond) // let any stale watcher act (it must not)
	if a.generation < 2 {
		t.Fatal("generation must increment per start")
	}
	if !a.verifyOwnership(second, a.generation) {
		t.Fatal("current generation must own the current process")
	}
	if a.verifyOwnership(first, a.generation) {
		t.Fatal("a stale watcher generation must fail the ownership check")
	}
	term, kill := second.SignalCounts()
	if term != 0 || kill != 0 {
		t.Fatalf("stale watcher must never signal the new process (term=%d kill=%d)", term, kill)
	}
	_ = a.Stop(context.Background())
}

// R06: a foreign or PID-reused process is never signalled; identity must
// match the registration snapshot exactly (PID + exe + start time).
func TestR06_ForeignOrReusedPIDRefused(t *testing.T) {
	f := newFixture(t)
	a, _ := f.adapter()
	// Record a registration, then drop the process handle: a later "same
	// PID, different start time" (PID reuse) must fail ownership.
	a.mu.Lock()
	a.active = provider.ProcessInfo{PID: 4321, ExePath: "/worker", StartTime: 1000}
	a.mu.Unlock()
	reused := NewMockProcess(4321, "/worker", 2000) // same PID+exe, NEW start time
	if a.verifyOwnership(reused, 1) {
		t.Fatal("PID reuse (different start time) must fail ownership")
	}
	foreign := NewMockProcess(4321, "/other/exe", 1000) // same PID+start, different exe
	if a.verifyOwnership(foreign, 1) {
		t.Fatal("exe mismatch must fail ownership")
	}
	wrongGen := NewMockProcess(4321, "/worker", 1000)
	if a.verifyOwnership(wrongGen, 2) {
		t.Fatal("generation mismatch must fail ownership")
	}
	term, kill := reused.SignalCounts()
	if term != 0 || kill != 0 {
		t.Fatal("a refused identity must never be signalled")
	}
}

// killProofProcess survives TERM and KILL — the worst-case stuck process.
type killProofProcess struct{ *MockProcess }

func (k *killProofProcess) Signal(sig os.Signal) error { return nil } // ignores everything

// R07: Stop never waits forever after SIGKILL; it returns ErrStopIncomplete
// within a bounded window.
func TestR07_BoundedWaitAfterKill(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Lifecycle.StopGraceSeconds = 1
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	hung := &killProofProcess{MockProcess: f.runner.Last()}
	hung.SetIgnoreSignals()
	// Replace the handle with the kill-proof variant so the adapter's TERM/
	// KILL are absorbed (worst-case stuck process).
	a.mu.Lock()
	a.proc = hung
	a.mu.Unlock()
	started := time.Now()
	err := a.Stop(context.Background())
	if !errors.Is(err, ErrStopIncomplete) {
		t.Fatalf("stuck process must yield ErrStopIncomplete, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 6*time.Second {
		t.Fatalf("bounded wait exceeded: %s", elapsed)
	}
}

// R08: a repeated Start never extends a running limit, and after a hard
// stop there is no hidden automatic restart.
func TestR08_NoLimitExtensionNoAutoRestart(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadlineBefore, _ := a.Status(context.Background())
	for i := 0; i < 3; i++ {
		if err := a.Start(context.Background()); err == nil {
			t.Fatal("repeated start while running must be refused")
		}
	}
	deadlineAfter, _ := a.Status(context.Background())
	if !deadlineBefore.DeadlineAt.Equal(*deadlineAfter.DeadlineAt) {
		t.Fatal("repeated starts must not move the hard-stop deadline")
	}
	// Hard stop fires at the original deadline; no auto restart appears.
	f.clock.Advance(time.Duration(WDTTHardStopSeconds+1) * time.Second)
	if !waitForState(a, provider.StateStopped, 2*time.Second) {
		t.Fatal("hard stop must stop the process")
	}
	if env := f.runner.StartedCount(); env != 1 {
		t.Fatalf("no hidden auto-restart allowed, started=%d", env)
	}
	if !strings.Contains(errString(a.ExitError()), "hard stop") {
		t.Fatalf("exit reason must name the hard stop: %v", a.ExitError())
	}
	// A NEW lab session is an explicit operator action (Stop already implied
	// by the hard stop; explicit Start starts a fresh deadline) — document
	// the semantics by asserting a fresh deadline only after explicit Start.
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("explicit new lab session must be allowed: %v", err)
	}
	st, _ := a.Status(context.Background())
	if st.DeadlineAt == nil || !st.DeadlineAt.After(*deadlineBefore.DeadlineAt) {
		t.Fatal("a new explicit session gets a fresh deadline")
	}
	_ = a.Stop(context.Background())
}

// B3: Stop honours context cancellation and reports it instead of hanging.
func TestStop_ContextCancelled(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Lifecycle.StopGraceSeconds = 30 // long grace; ctx must cut it
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runner.Last().SetIgnoreSignals()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	err := a.Stop(ctx)
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("cancelled stop must report context, got %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancelled stop must return promptly")
	}
}

// Stage C regression (R09): repeated Health calls must never freshen the
// manifest's original E2E measurement.
func TestR09_HealthNeverRefreshesEvidence(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	old := f.clock.Now().Add(-2 * defaultTTLForTest())
	f.manifest.Socks.TCP = provider.EvidenceE2EVerified
	f.manifest.Socks.UDP = provider.EvidenceE2EVerified
	f.manifest.Evidence.EvidenceURL = "https://example.invalid/e2e"
	f.manifest.Evidence.MeasuredAt = &old
	a, _ := f.adapter()
	for i := 0; i < 3; i++ {
		h, err := a.Health(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if h.TCPE2E != provider.EvidenceUnknown || h.UDPE2E != provider.EvidenceUnknown {
			t.Fatalf("health %d must downgrade stale E2E to UNKNOWN, got %s/%s", i, h.TCPE2E, h.UDPE2E)
		}
		if h.Details["evidence_measured_at"] != old.UTC().Format(time.RFC3339) {
			t.Fatalf("health %d must keep the ORIGINAL measured_at, got %s", i, h.Details["evidence_measured_at"])
		}
		if h.Details["tcp_e2e_fresh"] != "false" {
			t.Fatalf("health %d must flag evidence as not fresh", i)
		}
	}
}

func defaultTTLForTest() time.Duration { return provider.DefaultMeasurementTTL }

// waitForState polls Status until the wanted state appears.
func waitForState(a *Adapter, want provider.LifecycleState, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		st, _ := a.Status(context.Background())
		if st.State == want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// R11: TCP and UDP evidence are judged independently.
func TestR11_TCPUDPIndependentEvidence(t *testing.T) {
	m := provider.Manifest{Provider: "wdtt"}
	m.Evidence.EvidenceURL = "https://example.invalid/e2e"
	old := time.Now().Add(-time.Hour)
	m.Evidence.MeasuredAt = &old
	// TCP fresh, UDP stale:
	m.Socks.TCP = provider.EvidenceE2EVerified
	m.Socks.UDP = provider.EvidenceE2EVerified
	// Two manifests with different measured_at per axis is modelled by the
	// adapter as ONE measurement pair; independence is enforced at the
	// Health level: fresh pair keeps both, stale pair downgrades both, and
	// an absent UDP requirement cannot mask a stale TCP claim.
	now := time.Now()
	h := provider.Health{MeasuredAt: now.Add(-2 * provider.DefaultHealthTTL), TTL: provider.DefaultHealthTTL}
	h.TCPE2E = provider.EvidenceE2EVerified
	h.UDPE2E = provider.EvidenceSourceConfirmed
	if h.EffectiveTCP(now) != provider.EvidenceUnknown {
		t.Fatal("stale TCP E2E must downgrade")
	}
	if h.EffectiveUDP(now) != provider.EvidenceSourceConfirmed {
		t.Fatal("SOURCE_CONFIRMED must not be affected by freshness")
	}
}
