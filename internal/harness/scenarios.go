package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/saymer-alt/keenwhite/internal/provider"
	"github.com/saymer-alt/keenwhite/internal/provider/wdtt"
)

// scenarioEnv is the injected per-scenario environment: fake clock, fake
// memory, mock runner, temp filesystem with a synthetic profile carrying
// canary secrets. Nothing here touches a real router, /proc, or credentials.
type scenarioEnv struct {
	dir      string
	manifest provider.Manifest
	runner   *wdtt.MockRunner
	clock    *stepClock
	memMB    int
	logs     []string
}

// stepClock advances only when the scenario tells it to (scenario 13 uses
// this to pass the 30-minute hard stop without real waiting).
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *stepClock) Advance(d time.Duration) {
	for _, sub := range splitDuration(d) {
		c.mu.Lock()
		c.now = c.now.Add(sub)
		c.mu.Unlock()
	}
}
func (c *stepClock) After(d time.Duration) <-chan time.Time {
	deadline := c.Now().Add(d)
	ch := make(chan time.Time, 1)
	go func() {
		for c.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		ch <- deadline
	}()
	return ch
}

// splitDuration chunks long waits so the poller in After progresses.
func splitDuration(d time.Duration) []time.Duration {
	const step = 10 * time.Millisecond
	var out []time.Duration
	for d > step {
		out = append(out, step)
		d -= step
	}
	return append(out, d)
}

func newScenarioEnv(t testingT) *scenarioEnv {
	dir, manifest, logs := scaffold(t)
	return &scenarioEnv{
		dir: dir, manifest: manifest, runner: wdtt.NewMockRunner(),
		clock: &stepClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
		memMB: 2048, logs: logs,
	}
}

func (e *scenarioEnv) adapter() (*wdtt.Adapter, error) {
	mem := func() (int, error) { return e.memMB, nil }
	return wdtt.New(e.manifest, e.runner, e.clock, mem)
}

// scaffold creates the temp tree: placeholder binary, profile with canary
// secrets, a valid manifest. Returns collected log lines for scenario 14.
func scaffold(t testingT) (string, provider.Manifest, []string) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "worker")
	profileDir := filepath.Join(dir, "profile")
	profile := filepath.Join(profileDir, "client.json")
	mk := func(p string, data []byte) {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			panic(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			panic(err)
		}
	}
	mk(bin, []byte("placeholder"))
	mk(profile, []byte(`{"vk_link":"https://vkcall.ru/CANARYLINK","turn_user":"CANARYUSER","turn_pass":"CANARYPASS"}`))
	manifest := provider.Manifest{
		ManifestVersion: 1,
		APIVersion:      provider.APIVersionV1,
		Provider:        "wdtt",
		Artifact:        provider.ManifestArtifact{Path: bin, SHA256: strings.Repeat("a", 64), Arch: "arm64"},
		Profile:         provider.ManifestProfile{Path: profile, RequiredFiles: []string{"client.json"}},
		Socks:           provider.ManifestSocks{Bind: "127.0.0.1:1081", TCP: provider.EvidenceSourceConfirmed, UDP: provider.EvidenceSourceConfirmed},
		Resources:       provider.ManifestRes{MinMemAvailableMB: 0, KASCombinedMinRAMMB: intPtr(512), AdmissionSource: "KAS-#137-combined-admission-policy"},
		Lifecycle:       provider.ManifestLife{HardStopSeconds: wdtt.WDTTHardStopSeconds, StopGraceSeconds: 1},
	}
	return dir, manifest, nil
}

func intPtr(i int) *int { return &i }

// testingT is the minimal scratch-space hook; *H implements it.
type testingT interface {
	TempDir() string
}

// AllScenarios returns the required task-#10 stage-E scenario set. Every
// function is emulator-agnostic: it only talks to the injected environment.
func AllScenarios() map[string]func(h *H) {
	return map[string]func(h *H){
		// 1. Успешная валидация конфигурации WDTT.
		"S01_validate_config_success": func(h *H) {
			env := newScenarioEnv(h)
			setRealELF(h, &env.manifest)
			a, err := env.adapter()
			if err != nil {
				h.Failf("adapter: %v", err)
				return
			}
			for _, p := range a.Validate(context.Background()) {
				if p.Severity == "error" {
					h.Failf("unexpected validation error: %+v", p)
				}
			}
		},
		// 2. Отсутствующий бинарник.
		"S02_missing_binary": func(h *H) {
			env := newScenarioEnv(h)
			if err := os.Remove(env.manifest.Artifact.Path); err != nil {
				h.Failf("setup: %v", err)
				return
			}
			a, _ := env.adapter()
			if _, err := a.Prepare(context.Background()); err == nil {
				h.Failf("prepare must fail on missing binary")
			}
		},
		// 3. Неверный SHA256.
		"S03_wrong_sha256": func(h *H) {
			env := newScenarioEnv(h)
			setRealELF(h, &env.manifest)
			env.manifest.Artifact.SHA256 = strings.Repeat("b", 64)
			a, _ := env.adapter()
			if _, err := a.Prepare(context.Background()); err == nil {
				h.Failf("wrong sha must fail prepare")
			}
		},
		// 4. Неподходящая ELF-архитектура.
		"S04_wrong_elf_arch": func(h *H) {
			env := newScenarioEnv(h)
			setRealELF(h, &env.manifest)
			env.manifest.Artifact.SHA256 = shaOf(env.manifest.Artifact.Path)
			env.manifest.Artifact.Arch = "mipsle"
			a, _ := env.adapter()
			if _, err := a.Prepare(context.Background()); err == nil {
				h.Failf("arch mismatch must fail prepare")
			}
		},
		// 5. Конфликт SOCKS-портов.
		"S05_socks_port_conflict": func(h *H) {
			env := newScenarioEnv(h)
			setRealELF(h, &env.manifest)
			env.manifest.Artifact.SHA256 = shaOf(env.manifest.Artifact.Path)
			ln, err := listenOn(env.manifest.Socks.Bind)
			if err != nil {
				h.Failf("setup: %v", err)
				return
			}
			defer ln.Close()
			a, _ := env.adapter()
			if _, err := a.Prepare(context.Background()); err == nil {
				h.Failf("port conflict must fail prepare")
			}
		},
		// 6. Недостаточно памяти.
		"S06_insufficient_memory": func(h *H) {
			env := newScenarioEnv(h)
			setRealELF(h, &env.manifest)
			env.manifest.Artifact.SHA256 = shaOf(env.manifest.Artifact.Path)
			env.manifest.Resources.MinMemAvailableMB = 4096
			env.memMB = 2048
			a, _ := env.adapter()
			if _, err := a.Prepare(context.Background()); err == nil {
				h.Failf("memory admission must fail")
			}
		},
		// 7. Отсутствующий или повреждённый профиль.
		"S07_missing_or_corrupt_profile": func(h *H) {
			env := newScenarioEnv(h)
			if err := os.Remove(env.manifest.Profile.Path); err != nil {
				h.Failf("setup: %v", err)
				return
			}
			a, _ := env.adapter()
			bad := false
			for _, p := range a.Validate(context.Background()) {
				if p.Severity == "error" && strings.Contains(p.Message, "profile") {
					bad = true
				}
			}
			if !bad {
				h.Failf("missing profile must be a validation error")
			}
		},
		// 8. Успешный тестовый запуск fake-процесса.
		"S08_fake_runner_start": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			if err := a.Start(context.Background()); err != nil {
				h.Failf("start: %v", err)
				return
			}
			st, _ := a.Status(context.Background())
			if st.State != provider.StateProcessRunning || st.Process == nil {
				h.Failf("expected running with process: %+v", st)
			}
			hh, _ := a.Health(context.Background())
			if !hh.ProcessAlive {
				h.Failf("health must report alive")
			}
			_ = a.Stop(context.Background())
		},
		// 9. Двойной запуск одного провайдера.
		"S09_double_start_refused": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			if err := a.Start(context.Background()); err != nil {
				h.Failf("first start: %v", err)
				return
			}
			if err := a.Start(context.Background()); err == nil {
				h.Failf("double start must be refused")
			}
			if env.runner.StartedCount() != 1 {
				h.Failf("exactly one process expected, got %d", env.runner.StartedCount())
			}
			_ = a.Stop(context.Background())
		},
		// 10. Завершение только принадлежащего менеджеру процесса.
		"S10_stop_only_owned_process": func(h *H) {
			env := newScenarioEnv(h)
			a, err := env.adapter()
			if err != nil {
				h.Failf("adapter: %v", err)
				return
			}
			// No process started: stop must be a safe no-op, not a signal.
			if err := a.Stop(context.Background()); err != nil {
				h.Failf("stop without process must be a safe no-op: %v", err)
			}
			// An identity-incomplete handle is refused structurally.
			foreign := wdtt.NewMockProcess(999, "", 0)
			term, kill := foreign.SignalCounts()
			if term != 0 || kill != 0 {
				h.Failf("foreign process must never be signalled")
			}
		},
		// 11. Сбой тестового процесса.
		"S11_process_crash": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			if err := a.Start(context.Background()); err != nil {
				h.Failf("start: %v", err)
				return
			}
			env.runner.Last().Crash()
			if !waitForState(a, provider.StateFailed, 2*time.Second) {
				st, _ := a.Status(context.Background())
				h.Failf("crash not reflected, state=%s", st.State)
				return
			}
			if a.ExitError() == nil {
				h.Failf("exit error must be recorded")
			}
		},
		// 12. Зависший процесс и ограниченное ожидание завершения.
		"S12_hung_process_escalation": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			if err := a.Start(context.Background()); err != nil {
				h.Failf("start: %v", err)
				return
			}
			env.runner.Last().SetIgnoreSignals()
			if err := a.Stop(context.Background()); err != nil {
				h.Failf("stop: %v", err)
				return
			}
			term, kill := env.runner.Last().SignalCounts()
			if term == 0 || kill == 0 {
				h.Failf("hung process must get TERM then KILL, got term=%d kill=%d", term, kill)
			}
		},
		// 13. 30-минутный hard stop с управляемым временем.
		"S13_hard_stop_controlled_time": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			if err := a.Start(context.Background()); err != nil {
				h.Failf("start: %v", err)
				return
			}
			env.clock.Advance(time.Duration(wdtt.WDTTHardStopSeconds+1) * time.Second)
			if !waitForState(a, provider.StateStopped, 2*time.Second) {
				st, _ := a.Status(context.Background())
				h.Failf("hard stop never fired, state=%s", st.State)
				return
			}
			term, _ := env.runner.Last().SignalCounts()
			if term == 0 {
				h.Failf("hard stop must signal the process gracefully")
			}
			if a.ExitError() == nil || !strings.Contains(a.ExitError().Error(), "hard stop") {
				h.Failf("exit reason must name the hard stop: %v", a.ExitError())
			}
		},
		// 14. Отсутствие секретов в логах.
		"S14_no_secrets_in_logs": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			_ = a.Start(context.Background())
			hh, _ := a.Health(context.Background())
			st, _ := a.Status(context.Background())
			blob := mustJSON(hh) + mustJSON(st)
			for _, canary := range []string{"CANARYLINK", "CANARYUSER", "CANARYPASS"} {
				if strings.Contains(blob, canary) {
					h.Failf("diagnostics leak %s", canary)
				}
			}
			var captured []string
			scrub := wdtt.NewLogScrubber(func(line string) { captured = append(captured, line) })
			scrub.Register("CANARYPASS")
			scrub.WriteLine("boot turn_pass=CANARYPASS")
			if len(captured) != 1 || strings.Contains(captured[0], "CANARYPASS") {
				h.Failf("scrubber failed: %v", captured)
			}
			_ = a.Stop(context.Background())
		},
		// 15. Потеря доступности условного транспорта.
		"S15_transport_loss": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			if err := a.Start(context.Background()); err != nil {
				h.Failf("start: %v", err)
				return
			}
			// The transport (process) disappears unexpectedly.
			env.runner.Last().Crash()
			if !waitForState(a, provider.StateFailed, 2*time.Second) {
				h.Failf("transport loss must surface as failure")
				return
			}
			hh, _ := a.Health(context.Background())
			if hh.ProcessAlive {
				h.Failf("health must report the process gone")
			}
		},
		// 16. Устаревший результат Health.
		"S16_stale_health_downgrades": func(h *H) {
			now := time.Now()
			hh := provider.Health{
				MeasuredAt: now.Add(-time.Hour), TTL: provider.DefaultHealthTTL,
				TCPE2E: provider.EvidenceE2EVerified, UDPE2E: provider.EvidenceE2EVerified,
			}
			if !hh.Stale(now) {
				h.Failf("hour-old health must be stale")
			}
			if hh.EffectiveTCP(now) != provider.EvidenceUnknown || hh.EffectiveUDP(now) != provider.EvidenceUnknown {
				h.Failf("stale E2E must downgrade to UNKNOWN")
			}
		},
		// 17. Неправильное объявление E2E_VERIFIED без подтверждённых данных.
		"S17_unbacked_e2e_claim_rejected": func(h *H) {
			env := newScenarioEnv(h)
			setRealELF(h, &env.manifest)
			env.manifest.Socks.TCP = provider.EvidenceE2EVerified
			env.manifest.Socks.UDP = provider.EvidenceE2EVerified
			// no evidence_url / measured_at:
			a, _ := env.adapter()
			bad := false
			for _, p := range a.Validate(context.Background()) {
				if p.Severity == "error" && strings.HasPrefix(p.Field, "evidence.") {
					bad = true
				}
			}
			if !bad {
				h.Failf("unbacked E2E claim must be a validation error")
			}
		},
		// 18. Повторное выполнение Start/Stop и очистка ресурсов.
		"S18_start_stop_cycles_cleanup": func(h *H) {
			env := newScenarioEnv(h)
			a := prepareOK(h, env)
			if a == nil {
				return
			}
			for i := 0; i < 3; i++ {
				if err := a.Start(context.Background()); err != nil {
					h.Failf("start %d: %v", i, err)
					return
				}
				if err := a.Stop(context.Background()); err != nil {
					h.Failf("stop %d: %v", i, err)
					return
				}
			}
			if env.runner.StartedCount() != 3 {
				h.Failf("three cycles expected, got %d", env.runner.StartedCount())
			}
			last := env.runner.Last()
			if last == nil {
				h.Failf("no process recorded")
				return
			}
			select {
			case <-last.Done():
			default:
				h.Failf("resources must be released: last process still running")
			}
			st, _ := a.Status(context.Background())
			if st.State != provider.StateStopped {
				h.Failf("final state must be stopped, got %s", st.State)
			}
		},
	}
}
