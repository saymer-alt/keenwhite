package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validManifest() Manifest {
	return Manifest{
		ManifestVersion: 1,
		APIVersion:      APIVersionV1,
		Provider:        "wdtt",
		Artifact:        ManifestArtifact{Path: "bin/worker", SHA256: strings.Repeat("a", 64), Arch: "arm64"},
		Profile:         ManifestProfile{Path: "profile/client.json"},
		Socks:           ManifestSocks{Bind: "127.0.0.1:1081", TCP: EvidenceSourceConfirmed, UDP: EvidenceSourceConfirmed},
		Lifecycle:       ManifestLife{HardStopSeconds: 1800, StopGraceSeconds: 10},
	}
}

func writeManifest(t *testing.T, m any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidManifestPasses(t *testing.T) {
	m, problems, err := LoadManifest(writeManifest(t, validManifest()))
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	for _, p := range problems {
		if p.Severity == "error" {
			t.Fatalf("unexpected error problem: %+v", p)
		}
	}
	if m.Provider != "wdtt" || m.MeasurementTTL() != DefaultMeasurementTTL {
		t.Fatalf("bad parse: %+v", m)
	}
}

func TestManifestVersionMismatchRejected(t *testing.T) {
	m := validManifest()
	m.ManifestVersion = 2
	_, problems, err := LoadManifest(writeManifest(t, m))
	if err == nil {
		t.Fatal("manifest_version 2 must be rejected")
	}
	if len(problems) == 0 || problems[0].Field != "manifest_version" {
		t.Fatalf("expected manifest_version problem, got %+v", problems)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	doc := `{"manifest_version":1,"api_version":"keenwhite.provider/v1","provider":"wdtt",
	         "artifact":{"path":"b","sha256":"` + strings.Repeat("a", 64) + `","arch":"arm64"},
	         "profile":{"path":"p"},"socks":{"bind":"127.0.0.1:1081","tcp":"UNKNOWN","udp":"UNKNOWN"},
	         "resources":{},"lifecycle":{"hard_stop_seconds":1800},
	         "evidence":{},"surprise_field":true}`
	p := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadManifest(p); err == nil {
		t.Fatal("unknown fields must be rejected")
	}
}

func TestBindRulesNegative(t *testing.T) {
	for _, bind := range []string{"localhost:1081", "0.0.0.0:1081", "[::1]:1081", "127.0.0.2:1081", "127.0.0.1:80", "127.0.0.1:notaport"} {
		m := validManifest()
		m.Socks.Bind = bind
		problems := ValidateManifest(m)
		found := false
		for _, p := range problems {
			if p.Field == "socks.bind" {
				found = true
			}
		}
		if !found {
			t.Fatalf("bind %q must produce socks.bind problem", bind)
		}
	}
}

func TestE2EWithoutMeasurementRejected(t *testing.T) {
	// Fail-closed is sequential: the URL gate fires first...
	m := validManifest()
	m.Socks.TCP = EvidenceE2EVerified
	problems := ValidateManifest(m)
	if len(problems) == 0 || problems[0].Field != "evidence.evidence_url" {
		t.Fatalf("E2E without url must fail the URL gate first, got %+v", problems)
	}
	// ...and once the URL exists, the missing measurement gate fires.
	m.Evidence.EvidenceURL = "https://example.invalid/e2e"
	problems = ValidateManifest(m)
	if len(problems) == 0 || problems[0].Field != "evidence.measured_at" {
		t.Fatalf("E2E without measured_at must fail the date gate, got %+v", problems)
	}
}

func TestStaleMeasurementCannotClaimE2E(t *testing.T) {
	m := validManifest()
	old := time.Now().Add(-2 * DefaultMeasurementTTL)
	m.Socks.TCP = EvidenceE2EVerified
	m.Socks.UDP = EvidenceE2EVerified
	m.Evidence = ManifestEvid{EvidenceURL: "https://example.invalid/e2e", MeasuredAt: &old}
	problems := ValidateManifest(m)
	stale := false
	for _, p := range problems {
		if p.Field == "evidence.measured_at" && p.Severity == "error" && strings.Contains(p.Message, "stale") {
			stale = true
		}
	}
	if !stale {
		t.Fatalf("stale E2E measurement must be an error, got %+v", problems)
	}
}

func TestFreshE2EAccepted(t *testing.T) {
	m := validManifest()
	now := time.Now()
	m.Socks.TCP = EvidenceE2EVerified
	m.Evidence = ManifestEvid{EvidenceURL: "https://example.invalid/e2e", MeasuredAt: &now}
	for _, p := range ValidateManifest(m) {
		if p.Severity == "error" {
			t.Fatalf("fresh E2E must pass: %+v", p)
		}
	}
}

func TestHardStopMandatory(t *testing.T) {
	m := validManifest()
	m.Lifecycle.HardStopSeconds = 0
	problems := ValidateManifest(m)
	if len(problems) == 0 || problems[0].Field != "lifecycle.hard_stop_seconds" {
		t.Fatalf("hard stop is mandatory, got %+v", problems)
	}
}

func TestHealthStalenessDowngradesE2E(t *testing.T) {
	now := time.Now()
	h := Health{MeasuredAt: now.Add(-time.Hour), TTL: DefaultHealthTTL, TCPE2E: EvidenceE2EVerified, UDPE2E: EvidenceE2EVerified}
	if !h.Stale(now) {
		t.Fatal("hour-old health must be stale")
	}
	if h.EffectiveTCP(now) != EvidenceUnknown || h.EffectiveUDP(now) != EvidenceUnknown {
		t.Fatal("stale E2E health must downgrade to UNKNOWN")
	}
	fresh := Health{MeasuredAt: now, TTL: DefaultHealthTTL, TCPE2E: EvidenceE2EVerified}
	if fresh.Stale(now) || fresh.EffectiveTCP(now) != EvidenceE2EVerified {
		t.Fatal("fresh health must keep its evidence")
	}
	// Zero TTL falls back to the default, never to "never stale".
	zero := Health{MeasuredAt: now.Add(-DefaultHealthTTL - time.Second), TCPE2E: EvidenceE2EVerified}
	if !zero.Stale(now) {
		t.Fatal("zero TTL must use the default, not disable staleness")
	}
}

func TestStartPlanMarshalsWithoutSecrets(t *testing.T) {
	plan := StartPlan{
		Command: "/opt/worker", Args: []string{"-config", "/secret/profile.json"},
		EnvKeys: []string{"WDTT_CONFIG"}, ConfigPaths: []string{"/secret/profile.json"},
		SocksBind: "127.0.0.1:1081", HardStopSeconds: 1800, Executor: ExecutionMock,
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, banned := range []string{"token", "password", "credential", "turn_creds"} {
		if strings.Contains(strings.ToLower(text), banned) {
			t.Fatalf("start plan must not embed secret-like keys: %s", text)
		}
	}
	if !strings.Contains(text, `"env_keys"`) {
		t.Fatal("env must be names-only")
	}
}

func TestEvidenceAndStatesComplete(t *testing.T) {
	for _, e := range []Evidence{"UNKNOWN", "UNSUPPORTED", "SOURCE_CONFIRMED", "BUILD_CONFIRMED", "E2E_VERIFIED"} {
		if !Evidence(e).Valid() {
			t.Fatalf("evidence %s must be valid", e)
		}
	}
	if Evidence("PASS").Valid() || Evidence("").Valid() {
		t.Fatal("non-contract evidence must be invalid")
	}
	if StateProcessRunning == StateListenerReady || StateListenerReady == StateTrafficE2E {
		t.Fatal("process/listener/traffic states must stay distinct")
	}
	_ = fmt.Sprintf // keep fmt imported for future table tests
}

// R10 (issue #20): a future-dated E2E measurement is corrupt and must be
// rejected at validation time, before any adapter ever sees it.
func TestFutureDatedEvidenceRejected(t *testing.T) {
	m := validManifest()
	future := time.Now().Add(24 * time.Hour)
	m.Socks.TCP = EvidenceE2EVerified
	m.Socks.UDP = EvidenceE2EVerified
	m.Evidence = ManifestEvid{EvidenceURL: "https://example.invalid/e2e", MeasuredAt: &future}
	problems := ValidateManifest(m)
	found := false
	for _, p := range problems {
		if p.Field == "evidence.measured_at" && p.Severity == "error" && strings.Contains(p.Message, "future") {
			found = true
		}
	}
	if !found {
		t.Fatalf("future-dated measurement must be rejected, got %+v", problems)
	}
}
