// Package harness runs the KeenWhite provider test scenarios against
// injected environments. Today the environment is local and fake (mock
// runner, injected clock/memory); the same harness will later attach to a
// Keenetic emulator through a different Environment — no code changes to
// scenarios, no secrets, no real routers.
//
// Report contract (report_version 1) is JSON-safe and secret-free so CI can
// store it as an artifact:
//
//	GitHub Actions → Keenetic Emulator → KeenWhite Test Harness → PASS/FAIL
package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// ReportVersion is the harness report schema version.
const ReportVersion = 1

// Environment describes the execution environment handed to scenarios.
// "local-fake" is the default; a future Keenetic-emulator adapter supplies
// its own clock/memory/runner wiring through this struct — scenarios stay
// emulator-agnostic.
type Environment struct {
	Kind     string `json:"kind"`     // "local-fake"
	Emulator string `json:"emulator"` // "none" until an emulator exists
	Notes    string `json:"notes,omitempty"`
}

// DefaultEnvironment is the offline local-fake environment.
func DefaultEnvironment() Environment {
	return Environment{Kind: "local-fake", Emulator: "none",
		Notes: "mock runner, injected clock and memory; no real router, network or credentials"}
}

// ScenarioResult is one scenario outcome.
type ScenarioResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // PASS | FAIL
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Report is the machine-readable result for CI artifacts.
type Report struct {
	ReportVersion int         `json:"report_version"`
	Tool          string      `json:"tool"`
	APIVersion    string      `json:"api_version"`
	Environment   Environment `json:"environment"`
	// ResultSource labels WHERE these results came from (task 14 D3). It is
	// DERIVED from the environment kind, never asserted by scenarios: the
	// local-fake environment is always LOCAL_FAKE. No other kind is
	// accepted by Run until a verified emulator adapter exists, so a run
	// can never acquire the KEENETIC_EMULATOR label without the proof set
	// of emulator.RunProof.
	ResultSource string           `json:"result_source"`
	StartedAt    time.Time        `json:"started_at"`
	DurationMS   int64            `json:"duration_ms"`
	Scenarios    []ScenarioResult `json:"scenarios"`
	Summary      Summary          `json:"summary"`
}

// Summary aggregates pass/fail counts.
type Summary struct {
	Total int  `json:"total"`
	Pass  int  `json:"pass"`
	Fail  int  `json:"fail"`
	AllOK bool `json:"all_ok"`
}

// H is the per-scenario handle. A scenario that calls Failf records FAIL and
// returns; panics are recovered by the harness as failures.
type H struct {
	name   string
	failed bool
	detail string
	tmpDir string
}

// Failf marks the scenario failed with a formatted reason.
func (h *H) Failf(format string, args ...any) {
	h.failed = true
	if h.detail == "" {
		h.detail = fmt.Sprintf(format, args...)
	}
}

// Failed reports whether the scenario failed so far.
func (h *H) Failed() bool { return h.failed }

// Run executes scenarios in order against env and produces the report.
// Each scenario receives its own H; a panic becomes a FAIL with the panic
// message, never a harness crash.
func Run(env Environment, scenarios map[string]func(h *H)) Report {
	report := Report{
		ReportVersion: ReportVersion,
		Tool:          "keenwhite-harness",
		APIVersion:    "keenwhite.provider/v1",
		Environment:   env,
		StartedAt:     time.Now().UTC(),
	}
	// Environment gate (task 14 D3): only the verified local-fake
	// environment may run today. Any other kind produces a FAILING report —
	// never a silently mislabeled one.
	if env.Kind != "local-fake" {
		report.ResultSource = "UNVERIFIED"
		report.Scenarios = append(report.Scenarios, ScenarioResult{
			Name: "environment", Status: "FAIL",
			Detail: "environment kind " + env.Kind + " is not accepted: only local-fake runs until a verified emulator adapter supplies a complete RunProof",
		})
		report.Summary.Fail = 1
		report.Summary.Total = 1
		return report
	}
	report.ResultSource = "LOCAL_FAKE"
	names := make([]string, 0, len(scenarios))
	for name := range scenarios {
		names = append(names, name)
	}
	sortNames(names)
	for _, name := range names {
		start := time.Now()
		h := &H{name: name}
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.Failf("panic: %v", r)
				}
			}()
			scenarios[name](h)
		}()
		status := "PASS"
		if h.failed {
			status = "FAIL"
		}
		report.Scenarios = append(report.Scenarios, ScenarioResult{
			Name: name, Status: status, Detail: h.detail,
			DurationMS: time.Since(start).Milliseconds(),
		})
		if h.failed {
			report.Summary.Fail++
		} else {
			report.Summary.Pass++
		}
		report.Summary.Total++
	}
	report.Summary.AllOK = report.Summary.Fail == 0
	report.DurationMS = time.Since(report.StartedAt).Milliseconds()
	return report
}

// WriteReport stores the JSON report (CI artifact). It contains no secrets
// by construction: scenarios never put credentials into results.
func WriteReport(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// sortNames keeps report order stable (deterministic CI artifacts).
func sortNames(names []string) { sort.Strings(names) }
