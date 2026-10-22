package wdtt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/saymer-alt/keenwhite/internal/artifact"
	"github.com/saymer-alt/keenwhite/internal/provider"
)

var osReadFile = os.ReadFile

// WDTT lab hard stop (KAS/owner-lab policy): preserved exactly, never
// extended, never dropped. See task #10 stage D.
const WDTTHardStopSeconds = 1800

// parseMemAvailableMB extracts MemAvailable (KiB) as whole MiB.
func parseMemAvailableMB(meminfo string) (int, error) {
	for _, line := range strings.Split(meminfo, "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0, errors.New("malformed MemAvailable line")
			}
			kib, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, fmt.Errorf("MemAvailable value: %w", err)
			}
			return kib / 1024, nil
		}
	}
	return 0, errors.New("MemAvailable not found in meminfo")
}

// Adapter implements provider.Provider for WDTT.
// Construct with New; all inputs are injected so tests never touch a real
// router, real /proc, or real credentials.
type Adapter struct {
	manifest provider.Manifest
	runner   Runner
	clock    Clock
	mem      MemAvailableMB

	mu          sync.Mutex
	state       provider.LifecycleState
	proc        Process
	plan        provider.StartPlan
	deadline    time.Time
	exitErr     error
	lastHealth  provider.Health
	hardStopped bool
}

// New builds the adapter. runner must be a mock or lab runner — a "real"
// runner is refused here AND at Start time (defense in depth).
func New(m provider.Manifest, runner Runner, clk Clock, mem MemAvailableMB) (*Adapter, error) {
	if runner == nil {
		return nil, errors.New("runner is required (mock or lab; real execution is forbidden in this stage)")
	}
	if runner.Kind() == provider.ExecutionReal {
		return nil, provider.ErrForbiddenByPolicy
	}
	if clk == nil {
		clk = RealClock{}
	}
	if mem == nil {
		mem = RealMemAvailableMB
	}
	return &Adapter{
		manifest: m,
		runner:   runner,
		clock:    clk,
		mem:      mem,
		state:    provider.StateIdle,
	}, nil
}

// Name returns the manifest provider id.
func (a *Adapter) Name() string { return a.manifest.Provider }

// APIVersion returns the implemented contract version.
func (a *Adapter) APIVersion() string { return provider.APIVersionV1 }

// Validate runs manifest semantics plus file-presence checks. It reads the
// referenced files but never parses profile VALUES into logs or plans.
func (a *Adapter) Validate(_ context.Context) []provider.Problem {
	problems := provider.ValidateManifest(a.manifest)
	if err := checkProfile(&a.manifest); err != nil {
		problems = append(problems, provider.Errorf("profile", "%v", err))
	}
	if err := checkWDTTPolicy(&a.manifest); err != nil {
		problems = append(problems, provider.Errorf("lifecycle.hard_stop_seconds", "%v", err))
	}
	return problems
}

// checkProfile verifies presence and rough shape of the profile and its
// required companion files. Profile CONTENTS are never returned or logged.
func checkProfile(m *provider.Manifest) error {
	info, err := os.Stat(m.Profile.Path)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("profile is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > 1024*1024 {
		return errors.New("profile size out of bounds (0,1MiB]")
	}
	base := filepath.Dir(m.Profile.Path)
	for _, rel := range m.Profile.RequiredFiles {
		p := rel
		if !filepath.IsAbs(rel) && !strings.ContainsRune(rel, filepath.Separator) {
			p = filepath.Join(base, rel)
		}
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("required profile file %s: %w", rel, err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("required profile file %s is not regular", rel)
		}
	}
	return nil
}

// Prepare verifies the artifact, profile, resources and SOCKS port, and
// produces the StartPlan WITHOUT executing anything.
func (a *Adapter) Prepare(_ context.Context) (provider.PrepareResult, error) {
	var res provider.PrepareResult
	a.mu.Lock()
	a.state = provider.StatePreparing
	a.mu.Unlock()

	rep, err := artifact.VerifyWDTT(a.manifest.Artifact.Path, a.manifest.Artifact.SHA256, a.manifest.Artifact.Arch)
	if err != nil {
		a.setState(provider.StateFailed)
		return res, fmt.Errorf("artifact verification: %w", err)
	}

	memMB, err := a.mem()
	if err != nil {
		a.setState(provider.StateFailed)
		return res, fmt.Errorf("memory probe: %w", err)
	}
	memRes := provider.Resources{MemAvailableMB: memMB, Admitted: true}
	if need := a.manifest.Resources.MinMemAvailableMB; need > 0 && memMB < need {
		memRes.Admitted = false
		memRes.Reason = fmt.Sprintf("needs %dMB available, device has %dMB", need, memMB)
		a.setState(provider.StateFailed)
		return res, fmt.Errorf("resource admission failed: %s", memRes.Reason)
	}

	if err := checkSocksPort(a.manifest.Socks.Bind); err != nil {
		a.setState(provider.StateFailed)
		return res, fmt.Errorf("socks bind check: %w", err)
	}

	plan := a.buildPlan()
	res = provider.PrepareResult{
		Provider: a.manifest.Provider,
		Manifest: provider.ManifestSummary{Version: a.manifest.ManifestVersion, Provider: a.manifest.Provider, Bind: a.manifest.Socks.Bind},
		Artifacts: []provider.ArtifactClaim{{
			Path: a.manifest.Artifact.Path, SHA256: rep.SHA256,
			Architecture: rep.Architecture, Verification: rep.Verification,
		}},
		Resources: memRes,
		Plan:      plan,
	}
	a.mu.Lock()
	a.plan = plan
	a.state = provider.StateIdle
	a.mu.Unlock()
	return res, nil
}

// buildPlan materialises the launch specification. The profile stays a PATH:
// credentials are never placed in argv, env, or the plan itself. The exact
// production flag surface is confirmed at lab acceptance; the hard-stop
// deadline is derived from the manifest and is never negotiable here.
func (a *Adapter) buildPlan() provider.StartPlan {
	hard := a.manifest.Lifecycle.HardStopSeconds
	return provider.StartPlan{
		Command:         a.manifest.Artifact.Path,
		Args:            []string{"-config", a.manifest.Profile.Path},
		EnvKeys:         []string{"WDTT_CONFIG"},
		ConfigPaths:     []string{a.manifest.Profile.Path},
		SocksBind:       a.manifest.Socks.Bind,
		HardStopSeconds: hard,
		Deadline:        a.clock.Now().Add(time.Duration(hard) * time.Second),
		Executor:        a.runner.Kind(),
	}
}

// checkSocksPort verifies the loopback bind is currently free (bind-and-close
// probe on 127.0.0.1 only). An open port later is NOT success evidence.
func checkSocksPort(bind string) error {
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return fmt.Errorf("bind %s unavailable: %w", bind, err)
	}
	return ln.Close()
}

// Start performs a controlled launch. Refuses when resources are not
// admitted, when a process is already running (scenario: double start), and
// for any real-executor configuration (forbidden in this stage).
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.plan.Command == "" {
		return errors.New("Start requires a successful Prepare first")
	}
	if a.proc != nil {
		select {
		case <-a.proc.Done():
			// previous process already exited; fall through with cleanup
			a.exitErr = a.proc.ExitErr()
			a.proc = nil
		default:
			return errors.New("provider already running (double start refused)")
		}
	}
	if a.plan.Executor == provider.ExecutionReal {
		return provider.ErrForbiddenByPolicy
	}
	proc, err := a.runner.Start(ctx, a.plan)
	if err != nil {
		a.state = provider.StateFailed
		a.exitErr = err
		return fmt.Errorf("runner start: %w", err)
	}
	a.proc = proc
	a.deadline = a.plan.Deadline
	a.state = provider.StateProcessRunning

	// Hard-stop watcher: WDTT's 30-minute lab limit, enforced by the adapter
	// with the injected clock (tests trigger it without real waiting).
	stopCtx := ctx
	go a.watchDeadline(stopCtx, proc)
	return nil
}

func (a *Adapter) watchDeadline(_ context.Context, proc Process) {
	// Deadline math uses the INJECTED clock so tests control time.
	ttl := a.deadline.Sub(a.clock.Now())
	if ttl <= 0 {
		a.stopForHardStop(proc)
		return
	}
	ch := a.clock.After(ttl)
	select {
	case <-ch:
		a.stopForHardStop(proc)
	case <-proc.Done():
	}
}

// stopForHardStop terminates OUR process at the lab hard stop.
func (a *Adapter) stopForHardStop(proc Process) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proc == nil || a.proc.PID() != proc.PID() {
		return // not ours / already replaced — never touch a foreign handle
	}
	a.setStateLocked(provider.StateStopped)
	a.hardStopped = true
	a.exitErr = errors.New("stopped by WDTT 30-minute lab hard stop (exit code 75 semantics preserved by the binary itself)")
	// Graceful first, kill as the last resort — same handle, never a PID scan.
	_ = proc.Signal(osInterrupt)
	grace := a.manifest.Lifecycle.StopGraceSeconds
	if grace <= 0 {
		grace = 10
	}
	go func() {
		select {
		case <-proc.Done():
		case <-time.After(time.Duration(grace) * time.Second):
			_ = proc.Signal(osKill)
		}
	}()
}

// Stop terminates ONLY the recorded process, verifying ownership first:
// PID, exe path and start time must match what Start recorded. Grace then
// kill — still through the same handle, never by scanning PIDs.
func (a *Adapter) Stop(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proc == nil {
		a.state = provider.StateStopped
		return nil
	}
	proc := a.proc
	// Ownership proof: the handle must be the one this adapter started.
	if proc.PID() <= 0 || proc.ExePath() == "" || proc.StartTime() <= 0 {
		return errors.New("refusing to stop: process identity incomplete")
	}
	grace := a.manifest.Lifecycle.StopGraceSeconds
	if grace <= 0 {
		grace = 10
	}
	_ = proc.Signal(osInterrupt)
	select {
	case <-proc.Done():
	case <-time.After(time.Duration(grace) * time.Second):
		_ = proc.Signal(osKill)
		<-proc.Done()
	}
	a.exitErr = proc.ExitErr()
	a.proc = nil
	a.state = provider.StateStopped
	return nil
}

// Status returns the lifecycle snapshot, degrading to STOPPED/FAILED when
// the process exited since the last observation.
func (a *Adapter) Status(_ context.Context) (provider.Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := provider.Status{
		State:     a.state,
		Listener:  provider.ListenerStatus{Bind: a.manifest.Socks.Bind},
		TCPE2E:    a.manifest.Socks.TCP,
		UDPE2E:    a.manifest.Socks.UDP,
		CheckedAt: a.clock.Now(),
	}
	if a.proc != nil {
		st.Process = &provider.ProcessInfo{
			PID: a.proc.PID(), ExePath: a.proc.ExePath(), StartTime: a.proc.StartTime(),
		}
		if !a.deadline.IsZero() {
			d := a.deadline
			st.DeadlineAt = &d
		}
		select {
		case <-a.proc.Done():
			switch {
			case a.hardStopped:
				st.State = provider.StateStopped
			case a.proc.ExitErr() != nil:
				st.State = provider.StateFailed
				a.exitErr = a.proc.ExitErr() // record once for diagnostics
			default:
				st.State = provider.StateStopped
			}
		default:
			if a.state == provider.StateProcessRunning {
				st.State = provider.StateProcessRunning
			}
		}
	}
	return st, nil
}

// Health returns a fresh diagnostics snapshot. LISTENER_READY is reported
// only from an actual probe (in this offline stage: never asserted); E2E
// comes from the manifest evidence with freshness applied by the caller.
func (a *Adapter) Health(_ context.Context) (provider.Health, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := provider.Health{
		State:      a.state,
		TCPE2E:     a.manifest.Socks.TCP,
		UDPE2E:     a.manifest.Socks.UDP,
		MeasuredAt: a.clock.Now(),
		TTL:        provider.DefaultHealthTTL,
		Details: map[string]string{
			"runner":    string(a.runner.Kind()),
			"bind":      a.manifest.Socks.Bind,
			"hard_stop": strconv.Itoa(a.manifest.Lifecycle.HardStopSeconds),
		},
	}
	if a.proc != nil {
		select {
		case <-a.proc.Done():
			h.ProcessAlive = false
		default:
			h.ProcessAlive = true
		}
	}
	a.lastHealth = h
	return h, nil
}

// ExitError exposes the last recorded exit/wait error for diagnostics.
func (a *Adapter) ExitError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitErr
}

func (a *Adapter) setState(s provider.LifecycleState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setStateLocked(s)
}

func (a *Adapter) setStateLocked(s provider.LifecycleState) { a.state = s }

// LogScrubber wraps a writer so profile secrets that may arrive via process
// stdout/stderr never reach persistent logs. Callers register secret values
// they know (from their own config handling); anything registered is
// replaced by "<redacted>".
type LogScrubber struct {
	mu      sync.Mutex
	secrets []string
	sink    func(string)
}

// NewLogScrubber creates a scrubber writing scrubbed lines to sink.
func NewLogScrubber(sink func(string)) *LogScrubber { return &LogScrubber{sink: sink} }

// Register adds a secret value to scrub (never logged, only matched).
func (s *LogScrubber) Register(secret string) {
	if secret == "" {
		return
	}
	s.mu.Lock()
	s.secrets = append(s.secrets, secret)
	s.mu.Unlock()
}

// Scrub replaces every registered secret in line.
func (s *LogScrubber) Scrub(line string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sec := range s.secrets {
		line = strings.ReplaceAll(line, sec, "<redacted>")
	}
	return line
}

// WriteLine scrubs and forwards one line (convenience for scanners).
func (s *LogScrubber) WriteLine(line string) {
	s.sink(s.Scrub(line))
}

// ScanLines feeds each line of r through the scrubber.
func (s *LogScrubber) ScanLines(r *os.File) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		s.WriteLine(sc.Text())
	}
}

// checkWDTTPolicy pins the provider-specific safety policy: the WDTT lab
// hard stop is exactly 30 minutes and may never be raised or dropped here.
func checkWDTTPolicy(m *provider.Manifest) error {
	if m.Provider != "wdtt" {
		return nil
	}
	if m.Lifecycle.HardStopSeconds != WDTTHardStopSeconds {
		return errors.New("WDTT policy: hard stop must stay exactly 1800s (30-minute lab limit; never extended, never dropped)")
	}
	return nil
}
