package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAllScenarios runs the complete stage-E scenario set. When
// KW_HARNESS_REPORT is set, the machine-readable report is stored for CI
// artifacts (secret-free by construction).
func TestAllScenarios(t *testing.T) {
	env := DefaultEnvironment()
	report := Run(env, AllScenarios())
	for _, s := range report.Scenarios {
		if s.Status != "PASS" {
			t.Errorf("%s: %s (%s)", s.Name, s.Status, s.Detail)
		}
	}
	if !report.Summary.AllOK {
		t.Fatalf("summary: %+v", report.Summary)
	}
	if report.Summary.Total != 18 {
		t.Fatalf("expected 18 scenarios, got %d", report.Summary.Total)
	}
	// Sanity: environment is the offline fake; report must be marshalable.
	if report.Environment.Kind != "local-fake" || report.Environment.Emulator != "none" {
		t.Fatalf("unexpected environment: %+v", report.Environment)
	}
	if p := os.Getenv("KW_HARNESS_REPORT"); p != "" {
		if err := WriteReport(p, report); err != nil {
			t.Fatalf("write report: %v", err)
		}
	}
}

// TestReportIsSecretFree proves the report channel never carries profile
// values, even when a scenario touches a canary-bearing profile.
func TestReportIsSecretFree(t *testing.T) {
	report := Run(DefaultEnvironment(), AllScenarios())
	blob, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"CANARYLINK", "CANARYUSER", "CANARYPASS", "vkcall.ru"} {
		if stringContains(string(blob), canary) {
			t.Fatalf("report leaks %s", canary)
		}
	}
}

// TestReportWriteAndRead round-trips the artifact format CI will store.
func TestReportWriteAndRead(t *testing.T) {
	report := Run(DefaultEnvironment(), AllScenarios())
	p := filepath.Join(t.TempDir(), "harness-report.json")
	if err := WriteReport(p, report); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("report must round-trip: %v", err)
	}
	if back.ReportVersion != ReportVersion || back.Summary.Total != 18 {
		t.Fatalf("bad round-trip: %+v", back.Summary)
	}
}

func stringContains(hay, needle string) bool {
	return len(needle) == 0 || (len(hay) >= len(needle) && indexOf(hay, needle) >= 0)
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Task 14 D3: the result source is derived from the environment kind, and
// only local-fake may run. Any other kind — including a future
// "keenetic-emulator" label without a verified adapter — must produce a
// FAILING report, never a silently mislabeled passing one.
func TestUnverifiedEnvironmentKindFailsClosed(t *testing.T) {
	for _, kind := range []string{"keenetic-emulator", "qemu-user", ""} {
		env := Environment{Kind: kind, Emulator: "unknown"}
		report := Run(env, AllScenarios())
		if report.Summary.AllOK {
			t.Fatalf("kind %q must not produce an AllOK report", kind)
		}
		if report.ResultSource != "UNVERIFIED" {
			t.Fatalf("kind %q must be labeled UNVERIFIED, got %s", kind, report.ResultSource)
		}
		if report.Summary.Total != 1 || report.Scenarios[0].Name != "environment" {
			t.Fatalf("unverified kind must fail with a single environment finding: %+v", report)
		}
	}
}

// The default environment is always LOCAL_FAKE in the report.
func TestLocalFakeReportSource(t *testing.T) {
	report := Run(DefaultEnvironment(), AllScenarios())
	if report.ResultSource != "LOCAL_FAKE" {
		t.Fatalf("local-fake report must carry LOCAL_FAKE, got %s", report.ResultSource)
	}
}
