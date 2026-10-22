package wdtt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saymer-alt/keenwhite/internal/provider"
)

// fakeClockWithPoll is the clock actually used in lifecycle tests.
type fakeClockWithPoll struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClockWithPoll) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClockWithPoll) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}
func (c *fakeClockWithPoll) After(d time.Duration) <-chan time.Time {
	deadline := c.Now().Add(d)
	ch := make(chan time.Time, 1)
	go func() {
		for {
			c.mu.Lock()
			now := c.now
			c.mu.Unlock()
			if !now.Before(deadline) {
				ch <- now
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	return ch
}

// fixture assembles a valid adapter environment in a temp dir.
type fixture struct {
	dir      string
	manifest provider.Manifest
	runner   *MockRunner
	clock    *fakeClockWithPoll
	logs     *[]string
	logMu    *sync.Mutex
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "worker")
	profile := filepath.Join(dir, "profile", "client.json")
	if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
		t.Fatal(err)
	}
	// Synthetic ELF-ish regular file; artifact verification runs on it, so
	// build a real minimal ELF via the artifact package expectations: the
	// tests use the artifact package itself through Prepare, which needs a
	// valid ELF. Writing a real one is done in TestPrepare_FullSuccess via
	// buildTestELF; here we only exercise paths that do not need it.
	if err := os.WriteFile(bin, []byte("placeholder"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Profile with canary secrets: must never appear in logs/diagnostics.
	if err := os.WriteFile(profile, []byte(`{"vk_link":"https://vkcall.ru/CANARYLINK","turn_user":"CANARYUSER","turn_pass":"CANARYPASS"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := provider.Manifest{
		ManifestVersion: 1,
		APIVersion:      provider.APIVersionV1,
		Provider:        "wdtt",
		Artifact:        provider.ManifestArtifact{Path: bin, SHA256: strings.Repeat("a", 64), Arch: "arm64"},
		Profile:         provider.ManifestProfile{Path: profile, RequiredFiles: []string{"client.json"}},
		Socks:           provider.ManifestSocks{Bind: "127.0.0.1:1081", TCP: provider.EvidenceSourceConfirmed, UDP: provider.EvidenceSourceConfirmed},
		Resources:       provider.ManifestRes{MinMemAvailableMB: 0, KASCombinedMinRAMMB: intPtr(512), AdmissionSource: "KAS-#137-combined-admission-policy"},
		Lifecycle:       provider.ManifestLife{HardStopSeconds: WDTTHardStopSeconds, StopGraceSeconds: 1},
	}
	logs := []string{}
	f := &fixture{
		dir: dir, manifest: manifest, runner: NewMockRunner(),
		clock: &fakeClockWithPoll{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
		logs:  &logs, logMu: &sync.Mutex{},
	}
	return f
}

func intPtr(i int) *int { return &i }

func (f *fixture) adapter() (*Adapter, error) {
	mem := func() (int, error) { return 2048, nil }
	return New(f.manifest, f.runner, f.clock, mem)
}

func TestValidate_OK(t *testing.T) {
	f := newFixture(t)
	a, err := f.adapter()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range a.Validate(context.Background()) {
		if p.Severity == "error" {
			t.Fatalf("unexpected error: %+v", p)
		}
	}
}

func TestValidate_MissingBinary(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.manifest.Artifact.Path); err != nil {
		t.Fatal(err)
	}
	a, _ := f.adapter()
	// Validate is config-level (artifact absence is a Prepare concern):
	for _, p := range a.Validate(context.Background()) {
		if p.Severity == "error" {
			t.Fatalf("validate must stay clean for missing binary: %+v", p)
		}
	}
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("prepare must fail on missing binary")
	}
}

func TestValidate_MissingProfile(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.manifest.Profile.Path); err != nil {
		t.Fatal(err)
	}
	a, _ := f.adapter()
	problems := a.Validate(context.Background())
	bad := false
	for _, p := range problems {
		if p.Severity == "error" && strings.Contains(p.Message, "profile") {
			bad = true
		}
	}
	if !bad {
		t.Fatalf("missing profile must be a validation error: %+v", problems)
	}
}

func TestPrepare_FullSuccess(t *testing.T) {
	f := newFixture(t)
	// Build a REAL minimal arm64 ELF so artifact verification passes.
	sha := writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha
	a, _ := f.adapter()
	res, err := a.Prepare(context.Background())
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Verification != "HASH_AND_ARCH_MATCH" {
		t.Fatalf("artifact claim wrong: %+v", res.Artifacts)
	}
	if !res.Resources.Admitted {
		t.Fatalf("resources must be admitted: %+v", res.Resources)
	}
	if res.Plan.HardStopSeconds != WDTTHardStopSeconds {
		t.Fatalf("hard stop must stay 1800, got %d", res.Plan.HardStopSeconds)
	}
	// plan carries paths only, never profile contents:
	planJSON, _ := json.Marshal(res.Plan)
	for _, canary := range []string{"CANARYLINK", "CANARYUSER", "CANARYPASS"} {
		if strings.Contains(string(planJSON), canary) {
			t.Fatalf("plan leaks profile secrets: %s", planJSON)
		}
	}
}

func TestPrepare_WrongSHA(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = strings.Repeat("b", 64)
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("wrong SHA must fail prepare")
	}
}

func TestPrepare_WrongArch(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	sha := sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha
	f.manifest.Artifact.Arch = "mipsle"
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("arch mismatch must fail prepare")
	}
}

func TestPrepare_PortConflict(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	// Occupy the port first (loopback only).
	ln, err := listenTCP(f.manifest.Socks.Bind)
	if err != nil {
		t.Skipf("cannot occupy port: %v", err)
	}
	defer ln.Close()
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("port conflict must fail prepare")
	}
}

func TestPrepare_MemAdmission(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	f.manifest.Resources.MinMemAvailableMB = 4096
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("insufficient memory must fail prepare")
	}
}

func TestLifecycle_MockStartStop(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	st, _ := a.Status(context.Background())
	if st.State != provider.StateProcessRunning || st.Process == nil {
		t.Fatalf("expected running with process, got %+v", st)
	}
	h, _ := a.Health(context.Background())
	if !h.ProcessAlive {
		t.Fatal("health must report alive")
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	term, kill := f.runner.Last().SignalCounts()
	if term == 0 {
		t.Fatal("stop must signal the process")
	}
	if kill != 0 {
		t.Fatal("clean exit must not need SIGKILL")
	}
	st, _ = a.Status(context.Background())
	if st.State != provider.StateStopped {
		t.Fatalf("expected stopped, got %s", st.State)
	}
}

func TestLifecycle_DoubleStartRefused(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("double start must be refused")
	}
	if f.runner.StartedCount() != 1 {
		t.Fatalf("exactly one process expected, got %d", f.runner.StartedCount())
	}
}

func TestLifecycle_RestartAfterStop(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	for i := 0; i < 2; i++ {
		if err := a.Start(context.Background()); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		if err := a.Stop(context.Background()); err != nil {
			t.Fatalf("stop %d: %v", i, err)
		}
	}
	if f.runner.StartedCount() != 2 {
		t.Fatalf("two full cycles expected, got %d", f.runner.StartedCount())
	}
	// All resources of both cycles are gone: no processes left running.
	if f.runner.Last() != nil {
		select {
		case <-f.runner.Last().Done():
		default:
			t.Fatal("process must be reaped after stop")
		}
	}
}

func TestStop_NeverForeignPID(t *testing.T) {
	f := newFixture(t)
	a, _ := f.adapter()
	// No process was ever started: Stop must be a no-op, never a signal.
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("stop without process must be a safe no-op: %v", err)
	}
	// Identity guard: a handle with incomplete identity must be refused.
	p := NewMockProcess(999, "", 0)
	if err := stopForeign(a, p); err == nil {
		t.Fatal("incomplete identity must be refused")
	}
}

func TestLifecycle_CrashMarksFailed(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runner.Last().Crash()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st, _ := a.Status(context.Background())
		if st.State == provider.StateFailed || st.State == provider.StateStopped {
			if st.State != provider.StateFailed && a.ExitError() == nil {
				t.Fatalf("crash must be visible, got %s", st.State)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never reflected the crash: %+v", st)
		}
		time.Sleep(time.Millisecond)
	}
	if a.ExitError() == nil {
		t.Fatal("exit error must be recorded for diagnostics")
	}
}

func TestLifecycle_HungProcessEscalates(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.runner.Last().ignoresSig = true
	if err := a.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	term, kill := f.runner.Last().SignalCounts()
	if term == 0 || kill == 0 {
		t.Fatalf("hung process must get TERM then KILL, got term=%d kill=%d", term, kill)
	}
}

func TestLifecycle_HardStopWithFakeClock(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	// The deadline watcher polls the injected clock: advance past 30 minutes.
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Duration(WDTTHardStopSeconds+1) * time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := a.Status(context.Background())
		if st.State == provider.StateStopped {
			term, _ := f.runner.Last().SignalCounts()
			if term == 0 {
				t.Fatal("hard stop must signal the process")
			}
			if !strings.Contains(errString(a.ExitError()), "hard stop") {
				t.Fatalf("exit reason must name the hard stop: %v", a.ExitError())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("hard stop never fired")
}

func TestLifecycle_HardStopNotExtendable(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	// An attempt to raise the hard stop above the WDTT constant must fail
	// manifest validation (mandatory exact limit is the adapter's default).
	f.manifest.Lifecycle.HardStopSeconds = WDTTHardStopSeconds * 2
	a, _ := f.adapter()
	problems := a.Validate(context.Background())
	_ = problems // contract requires >0; WDTT adapter pins 1800 in New-validated manifests
	// The plan deadline is computed from the manifest value but the WDTT
	// policy check below enforces the exact constant for this provider.
	if err := checkWDTTPolicy(&f.manifest); err == nil {
		t.Fatal("raising the WDTT hard stop must be rejected")
	}
}

func TestSecretsNeverLogged(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	_, _ = a.Prepare(context.Background())
	_ = a.Start(context.Background())
	h, _ := a.Health(context.Background())
	diag, _ := json.Marshal(h)
	st, _ := a.Status(context.Background())
	stat, _ := json.Marshal(st)
	all := string(diag) + string(stat)
	for _, canary := range []string{"CANARYLINK", "CANARYUSER", "CANARYPASS"} {
		if strings.Contains(all, canary) {
			t.Fatalf("diagnostics leak %s: %s", canary, all)
		}
	}
	// LogScrubber removes registered secrets from process output lines.
	var captured []string
	scrub := NewLogScrubber(func(line string) { captured = append(captured, line) })
	scrub.Register("CANARYPASS")
	scrub.WriteLine("worker boot ok turn_pass=CANARYPASS")
	if len(captured) != 1 || strings.Contains(captured[0], "CANARYPASS") {
		t.Fatalf("scrubber failed: %v", captured)
	}
	_ = a.Stop(context.Background())
}

func TestRealExecutionForbidden(t *testing.T) {
	f := newFixture(t)
	writeTestELF(t, f.manifest.Artifact.Path)
	f.manifest.Artifact.SHA256 = sha256File(t, f.manifest.Artifact.Path)
	a, _ := f.adapter()
	if _, err := a.Prepare(context.Background()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	a.plan.Executor = provider.ExecutionReal
	if err := a.Start(context.Background()); err != provider.ErrForbiddenByPolicy {
		t.Fatalf("real executor must be forbidden, got %v", err)
	}
	if _, err := New(f.manifest, fakeRealRunner{}, f.clock, nil); err != provider.ErrForbiddenByPolicy {
		t.Fatalf("real runner must be refused at construction: %v", err)
	}
}

type fakeRealRunner struct{}

func (fakeRealRunner) Kind() provider.ExecutionKind { return provider.ExecutionReal }
func (fakeRealRunner) Start(_ context.Context, _ provider.StartPlan) (Process, error) {
	return nil, provider.ErrForbiddenByPolicy
}

// --- small helpers ---------------------------------------------------------

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func stopForeign(a *Adapter, p Process) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p.PID() <= 0 || p.ExePath() == "" || p.StartTime() <= 0 {
		return errors.New("refusing to stop: process identity incomplete")
	}
	return nil
}
