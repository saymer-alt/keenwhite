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
// extended, never dropped. See task #10 stage D / task #11 stage B.
const WDTTHardStopSeconds = 1800

// ErrStopIncomplete is returned when a process survived SIGKILL past the
// bounded post-kill wait. Callers must never wait indefinitely.
var ErrStopIncomplete = errors.New("stop incomplete: process survived SIGKILL past the bounded wait")

// ErrPlanTampered is returned when the prepared plan no longer matches the
// manifest the adapter validated (mutation between Prepare and Start).
var ErrPlanTampered = errors.New("plan tampered: prepared plan does not match the validated manifest")

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
//
// Admission is enforced at EVERY method boundary (Validate, Prepare, Start):
// safety never depends on the caller calling Validate first (task #11 B1).
// The plan produced by Prepare is a snapshot; Start re-derives the expected
// plan and refuses on any mismatch (R03). Every start increments a
// generation; the hard-stop watcher and Stop act only on the CURRENT
// generation and the recorded identity, so a stale watcher can never touch a
// new process (B3/R05/R06).
type Adapter struct {
	manifest provider.Manifest
	runner   Runner
	clock    Clock
	mem      MemAvailableMB

	mu          sync.Mutex
	state       provider.LifecycleState
	proc        Process
	active      provider.ProcessInfo // identity recorded at Start
	generation  uint64               // increments on every successful Start
	plan        provider.StartPlan
	preparedOK  bool
	deadline    time.Time
	hardStopped bool
	exitErr     error
}

// New builds the adapter. runner must be a mock, or an ExecRunner created
// WITH a lab grant — a grantless or "real" runner is refused here AND at
// Start time (defense in depth).
func New(m provider.Manifest, runner Runner, clk Clock, mem MemAvailableMB) (*Adapter, error) {
	if runner == nil {
		return nil, errors.New("runner is required (mock or granted lab; real execution is forbidden in this stage)")
	}
	if m.Provider != "wdtt" {
		return nil, fmt.Errorf("adapter identity mismatch: the WDTT adapter only accepts the 'wdtt' provider, got %q", m.Provider)
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

// validateAll is the single admission funnel: manifest semantics, profile
// presence, and the WDTT hard-stop policy. Validate, Prepare and Start all
// run it — bypassing Validate bypasses nothing (task #11 B1).
func (a *Adapter) validateAll() []provider.Problem {
	problems := provider.ValidateManifest(a.manifest)
	if a.manifest.Provider != "wdtt" {
		problems = append(problems, provider.Errorf("provider",
			"adapter identity mismatch: the WDTT adapter only accepts the 'wdtt' provider"))
	}
	if err := checkProfile(&a.manifest); err != nil {
		problems = append(problems, provider.Errorf("profile", "%v", err))
	}
	if err := checkWDTTPolicy(&a.manifest); err != nil {
		problems = append(problems, provider.Errorf("lifecycle.hard_stop_seconds", "%v", err))
	}
	return problems
}

// Validate runs the full admission checks. It reads the referenced files but
// never parses profile VALUES into logs or plans.
func (a *Adapter) Validate(_ context.Context) []provider.Problem {
	return a.validateAll()
}

// fullAdmission is the COMPLETE pre-launch check set, shared verbatim by
// Prepare and Start (task #12 A1): manifest semantics + provider identity +
// profile + hard-stop policy, artifact existence/integrity (SHA256 + ELF
// architecture), resource admission, and loopback SOCKS port availability.
// Start runs this immediately before spawning, so a stale Prepare can never
// authorise a launch whose world has changed. Note: the bind probe is
// necessarily point-in-time; exclusive port reservation until the transport
// binds requires descriptor passing and is a documented future design.
func (a *Adapter) fullAdmission() error {
	for _, p := range a.validateAll() {
		if p.Severity == "error" {
			return fmt.Errorf("admission failed: %s: %s", p.Field, p.Message)
		}
	}
	if _, err := artifact.VerifyWDTT(a.manifest.Artifact.Path, a.manifest.Artifact.SHA256, a.manifest.Artifact.Arch); err != nil {
		return fmt.Errorf("artifact verification: %w", err)
	}
	if _, _, err := a.checkResources(); err != nil {
		return err
	}
	if err := checkSocksPort(a.manifest.Socks.Bind); err != nil {
		return fmt.Errorf("socks bind check: %w", err)
	}
	return checkWDTTPolicy(&a.manifest)
}

// checkResources probes MemAvailable (injected) against the provider floor.
func (a *Adapter) checkResources() (int, provider.Resources, error) {
	memMB, err := a.mem()
	if err != nil {
		return 0, provider.Resources{}, fmt.Errorf("memory probe: %w", err)
	}
	res := provider.Resources{MemAvailableMB: memMB, Admitted: true}
	if need := a.manifest.Resources.MinMemAvailableMB; need > 0 && memMB < need {
		res.Admitted = false
		res.Reason = fmt.Sprintf("needs %dMB available, device has %dMB", need, memMB)
		return memMB, res, fmt.Errorf("resource admission failed: %s", res.Reason)
	}
	return memMB, res, nil
}

// checkWDTTPolicy pins the provider-specific safety policy: the WDTT lab
// hard stop is exactly 30 minutes and may never be raised, lowered or
// dropped here.
func checkWDTTPolicy(m *provider.Manifest) error {
	if m.Provider != "wdtt" {
		return nil
	}
	if m.Lifecycle.HardStopSeconds != WDTTHardStopSeconds {
		return errors.New("WDTT policy: hard stop must stay exactly 1800s (30-minute lab limit; never extended, never dropped)")
	}
	return nil
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

// Prepare verifies admission (again, independently of Validate), the
// artifact, profile, resources and SOCKS port, and produces the StartPlan
// WITHOUT executing anything. A successful Prepare is NOT a standing
// permission: Start re-validates everything under its own lock (B1).
// Prepare is refused while a process is running, so the plan of a live
// provider cannot be re-derived underneath it.
func (a *Adapter) Prepare(_ context.Context) (provider.PrepareResult, error) {
	var res provider.PrepareResult
	a.mu.Lock()
	defer a.mu.Unlock()
	// A2 (task #12): invalidate the previous grant BEFORE anything else —
	// a failed re-Prepare must leave no usable StartPlan behind.
	a.plan = provider.StartPlan{}
	a.preparedOK = false
	// A live session is refused FIRST and leaves its state untouched (A2.8).
	if a.proc != nil {
		select {
		case <-a.proc.Done():
		default:
			return res, errors.New("Prepare refused: provider is running; stop it first")
		}
	}
	// B1/A1: admission is mandatory here, order-independent, and identical
	// to what Start re-runs.
	if err := a.fullAdmission(); err != nil {
		a.state = provider.StateFailed
		return res, err
	}
	a.state = provider.StatePreparing

	rep, err := artifact.VerifyWDTT(a.manifest.Artifact.Path, a.manifest.Artifact.SHA256, a.manifest.Artifact.Arch)
	if err != nil {
		a.state = provider.StateFailed
		return res, fmt.Errorf("artifact verification: %w", err)
	}

	_, memRes, err := a.checkResources()
	if err != nil {
		a.state = provider.StateFailed
		return res, err
	}

	if err := checkSocksPort(a.manifest.Socks.Bind); err != nil {
		a.state = provider.StateFailed
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
	a.plan = plan
	a.preparedOK = true
	a.state = provider.StateIdle
	return res, nil
}

// buildPlan materialises the launch specification. The profile stays a PATH:
// credentials are never placed in argv, env, or the plan itself. The deadline
// is NOT fixed here — Start stamps it at the moment of the permitted launch
// (task #11 B4), so a stale Prepare can never age the limit.
func (a *Adapter) buildPlan() provider.StartPlan {
	return provider.StartPlan{
		Command:         a.manifest.Artifact.Path,
		Args:            []string{"-config", a.manifest.Profile.Path},
		EnvKeys:         []string{"WDTT_CONFIG"},
		ConfigPaths:     []string{a.manifest.Profile.Path},
		SocksBind:       a.manifest.Socks.Bind,
		HardStopSeconds: a.manifest.Lifecycle.HardStopSeconds,
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

// verifyPlanIntegrity re-derives the expected plan from the CURRENT manifest
// and compares it with the prepared snapshot. Any mutation of the manifest or
// of the prepared plan between Prepare and Start is refused (R03).
func (a *Adapter) verifyPlanIntegrity() error {
	if !a.preparedOK {
		return errors.New("Start requires a successful Prepare first")
	}
	expected := a.buildPlan()
	// The deadline is stamped at Start; everything else must match exactly.
	expected.Deadline = a.plan.Deadline
	if a.plan.Command != expected.Command ||
		strings.Join(a.plan.Args, "\x00") != strings.Join(expected.Args, "\x00") ||
		strings.Join(a.plan.EnvKeys, "\x00") != strings.Join(expected.EnvKeys, "\x00") ||
		a.plan.SocksBind != expected.SocksBind ||
		a.plan.HardStopSeconds != expected.HardStopSeconds ||
		a.plan.Executor != expected.Executor {
		return ErrPlanTampered
	}
	return checkWDTTPolicy(&a.manifest)
}

// Start performs a controlled launch. Re-validates admission (order-
// independence, R01/R02), refuses plan tampering (R03), refuses double
// start (a repeated start never extends a running limit, R08), stamps the
// hard-stop deadline at the moment of the permitted launch (B4), and
// increments the generation so stale watchers are inert for the new
// process (R05).
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// A1: re-run the FULL admission (artifact integrity, resources, port,
	// manifest, policy) immediately before spawning — never trust a stale
	// Prepare. No external command runs unless every check passes.
	if err := a.fullAdmission(); err != nil {
		return err
	}
	if err := a.verifyPlanIntegrity(); err != nil {
		return err
	}
	if a.proc != nil {
		select {
		case <-a.proc.Done():
			a.exitErr = a.proc.ExitErr()
			a.proc = nil
		default:
			return errors.New("provider already running (double start refused; a repeated start never extends the running limit)")
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
	a.generation++
	a.proc = proc
	a.active = provider.ProcessInfo{PID: proc.PID(), ExePath: proc.ExePath(), StartTime: proc.StartTime()}
	// B4: the deadline is computed from the real permitted-launch moment.
	a.deadline = a.clock.Now().Add(time.Duration(a.plan.HardStopSeconds) * time.Second)
	a.state = provider.StateProcessRunning
	a.hardStopped = false

	gen := a.generation
	go a.watchDeadline(ctx, proc, a.deadline, gen)
	return nil
}

func (a *Adapter) watchDeadline(_ context.Context, proc Process, deadline time.Time, gen uint64) {
	// Deadline math uses the INJECTED clock so tests control time.
	ttl := deadline.Sub(a.clock.Now())
	if ttl <= 0 {
		a.stopForHardStop(proc, gen)
		return
	}
	ch := a.clock.After(ttl)
	select {
	case <-ch:
		a.stopForHardStop(proc, gen)
	case <-proc.Done():
	}
}

// verifyOwnership proves the handle still describes EXACTLY the process
// recorded at Start of the CURRENT generation: same PID, same exe path, same
// start time (a reused PID has a different start time), same generation.
// Merely non-empty identity fields are NOT sufficient — they must equal the
// registration snapshot.
func (a *Adapter) verifyOwnership(proc Process, gen uint64) bool {
	if a.generation != gen || a.proc == nil || a.proc.PID() != proc.PID() {
		return false
	}
	return proc.PID() == a.active.PID &&
		proc.ExePath() == a.active.ExePath &&
		proc.StartTime() == a.active.StartTime
}

// stopForHardStop terminates OUR process at the lab hard stop — only if it
// still belongs to the current generation and matches the recorded identity.
func (a *Adapter) stopForHardStop(proc Process, gen uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.verifyOwnership(proc, gen) {
		return // stale watcher / reused PID / foreign handle — never signal
	}
	a.state = provider.StateStopped
	a.hardStopped = true
	a.exitErr = errors.New("stopped by WDTT 30-minute lab hard stop (exit code 75 semantics preserved by the binary itself)")
	terminateBounded(proc, time.Duration(a.manifest.Lifecycle.StopGraceSeconds)*time.Second, a.clock)
}

// Stop terminates ONLY the recorded process, verifying ownership first:
// current generation, PID, exe path and start time must match what Start
// recorded. The sequence is bounded: TERM → grace → KILL → short bounded
// collect, after which ErrStopIncomplete is returned instead of waiting
// forever. ctx cancellation is honoured at every stage.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.proc == nil {
		a.state = provider.StateStopped
		a.mu.Unlock()
		return nil
	}
	proc := a.proc
	gen := a.generation
	if !a.verifyOwnership(proc, gen) {
		a.mu.Unlock()
		return errors.New("refusing to stop: process identity does not match the recorded owner")
	}
	grace := a.manifest.Lifecycle.StopGraceSeconds
	if grace <= 0 {
		grace = 10
	}
	a.mu.Unlock()

	_ = proc.Signal(osInterrupt)
	select {
	case <-proc.Done():
	case <-time.After(time.Duration(grace) * time.Second):
		_ = proc.Signal(osKill)
		select {
		case <-proc.Done():
		case <-time.After(2 * time.Second):
			a.mu.Lock()
			a.state = provider.StateFailed
			a.exitErr = ErrStopIncomplete
			a.mu.Unlock()
			return ErrStopIncomplete
		case <-ctx.Done():
			a.mu.Lock()
			a.state = provider.StateFailed
			a.exitErr = fmt.Errorf("stop cancelled by context: %w", ctx.Err())
			a.mu.Unlock()
			return fmt.Errorf("stop cancelled by context: %w", ctx.Err())
		}
	case <-ctx.Done():
		return fmt.Errorf("stop cancelled by context: %w", ctx.Err())
	}

	a.mu.Lock()
	a.exitErr = proc.ExitErr()
	a.proc = nil
	a.state = provider.StateStopped
	a.mu.Unlock()
	return nil
}

// terminateBounded is the shared TERM → grace → KILL sequence. The final
// collect after KILL is bounded by the same grace; no caller ever blocks
// indefinitely on a stuck process.
func terminateBounded(proc Process, grace time.Duration, clk Clock) {
	if grace <= 0 {
		grace = 10 * time.Second
	}
	_ = proc.Signal(osInterrupt)
	select {
	case <-proc.Done():
		return
	case <-time.After(grace):
		_ = proc.Signal(osKill)
		select {
		case <-proc.Done():
		case <-clk.After(grace):
			// bounded give-up; the stopping entry point records its own
			// error semantics
		}
	}
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

// Health returns a fresh diagnostics snapshot of PROCESS and POLICY facts.
// It does NOT refresh E2E evidence: the TCP/UDP levels carry the manifest's
// ORIGINAL measurement time (task #11 stage C) and are downgraded to
// UNKNOWN when stale via EffectiveTCP/EffectiveUDP against the caller's
// clock. LISTENER_READY is reported only from a real probe — none exists in
// this offline stage, so it is always false here; a running process alone is
// never listener-ready, and a listener is never E2E.
func (a *Adapter) Health(_ context.Context) (provider.Health, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clock.Now()
	h := provider.Health{
		State:         a.state,
		TCPE2E:        a.manifest.Socks.TCP,
		UDPE2E:        a.manifest.Socks.UDP,
		MeasuredAt:    now,
		TTL:           provider.DefaultHealthTTL,
		ListenerReady: false,
		Details: map[string]string{
			"runner":               string(a.runner.Kind()),
			"bind":                 a.manifest.Socks.Bind,
			"hard_stop":            strconv.Itoa(a.manifest.Lifecycle.HardStopSeconds),
			"generation":           strconv.FormatUint(a.generation, 10),
			"evidence_measured_at": evidenceMeasuredAtUTC(&a.manifest),
		},
	}
	// Effective evidence: original measurement time + TTL + URL, fail-closed.
	tcpFresh := evidenceFresh(&a.manifest, now)
	h.Details["tcp_e2e_fresh"] = strconv.FormatBool(tcpFresh)
	h.Details["udp_e2e_fresh"] = strconv.FormatBool(tcpFresh)
	if !tcpFresh {
		if a.manifest.Socks.TCP == provider.EvidenceE2EVerified {
			h.TCPE2E = provider.EvidenceUnknown
		}
		if a.manifest.Socks.UDP == provider.EvidenceE2EVerified {
			h.UDPE2E = provider.EvidenceUnknown
		}
	}
	if a.proc != nil {
		select {
		case <-a.proc.Done():
			h.ProcessAlive = false
		default:
			h.ProcessAlive = true
		}
	}
	return h, nil
}

// evidenceMeasuredAtUTC renders the manifest's ORIGINAL measurement time
// (or "never") — display only, never a freshness claim. Health() calls do
// not move this value (task #11 stage C, R09).
func evidenceMeasuredAtUTC(m *provider.Manifest) string {
	if m.Evidence.MeasuredAt == nil {
		return "never"
	}
	return m.Evidence.MeasuredAt.UTC().Format(time.RFC3339)
}

// evidenceFresh fails closed for E2E levels: a URL and a PAST measured_at
// inside the manifest TTL are required; missing, future-dated or stale
// evidence makes any E2E claim NOT fresh (caller downgrades to UNKNOWN).
func evidenceFresh(m *provider.Manifest, now time.Time) bool {
	if m.Socks.TCP != provider.EvidenceE2EVerified && m.Socks.UDP != provider.EvidenceE2EVerified {
		return false
	}
	if m.Evidence.EvidenceURL == "" || m.Evidence.MeasuredAt == nil {
		return false
	}
	measured := *m.Evidence.MeasuredAt
	if measured.After(now.Add(5 * time.Minute)) { // future-dated = corrupt
		return false
	}
	return now.Sub(measured) <= m.MeasurementTTL()
}

// ExitError exposes the last recorded exit/wait error for diagnostics.
func (a *Adapter) ExitError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitErr
}

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
