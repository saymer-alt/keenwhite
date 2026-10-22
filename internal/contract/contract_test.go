package contract

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func TestCheckEvidence_FreshE2EPasses(t *testing.T) {
	measured := now.Add(-24 * time.Hour)
	eff, f := CheckEvidence(EvidenceE2EVerified, "https://e.example/x", &measured, 0, now)
	if eff != EvidenceE2EVerified || len(f) != 0 {
		t.Fatalf("fresh E2E must pass: %s %+v", eff, f)
	}
}

func TestCheckEvidence_FailClosed(t *testing.T) {
	cases := []struct {
		name     string
		level    string
		url      string
		measured *time.Time
		code     string
	}{
		{"no url", EvidenceE2EVerified, "", ptrTime(now.Add(-time.Hour)), "E2E_NO_URL"},
		{"no date", EvidenceE2EVerified, "https://e.example/x", nil, "E2E_NO_MEASUREMENT"},
		{"future", EvidenceE2EVerified, "https://e.example/x", ptrTime(now.Add(time.Hour)), "E2E_FUTURE_DATE"},
		{"stale", EvidenceE2EVerified, "https://e.example/x", ptrTime(now.Add(-91 * 24 * time.Hour)), "E2E_STALE"},
	}
	for _, c := range cases {
		eff, f := CheckEvidence(c.level, c.url, c.measured, 0, now)
		if eff != EvidenceUnknown {
			t.Fatalf("%s: must downgrade to UNKNOWN, got %s", c.name, eff)
		}
		found := false
		for _, x := range f {
			if x.Code == c.code {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: missing finding %s, got %+v", c.name, c.code, f)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestCheckEvidence_NonE2EPassThrough(t *testing.T) {
	for _, lvl := range []string{EvidenceUnknown, EvidenceUnsupported, EvidenceSourceConfirmed, EvidenceBuildConfirmed} {
		eff, f := CheckEvidence(lvl, "", nil, 0, now)
		if eff != lvl || len(f) != 0 {
			t.Fatalf("%s must pass through untouched", lvl)
		}
	}
	if _, f := CheckEvidence("PASS", "", nil, 0, now); len(f) == 0 || f[0].Code != "EVIDENCE_LEVEL" {
		t.Fatal("unknown level must be flagged")
	}
}

func TestCheckProviderSet(t *testing.T) {
	refs := []ProviderRef{
		{ID: "wdtt", Bind: "127.0.0.1:1081"},
		{ID: "wdtt", Bind: "127.0.0.1:1081"}, // duplicate id AND bind
		{ID: "openflux", Bind: "127.0.0.1:1081"},
	}
	f := CheckProviderSet(refs)
	codes := map[string]int{}
	for _, x := range f {
		codes[x.Code]++
	}
	if codes["DUPLICATE_PROVIDER"] != 1 || codes["BIND_CONFLICT"] != 1 {
		t.Fatalf("expected 1 dup + 1 bind conflict, got %+v", f)
	}
}

func TestParseMT_UnknownSchemaVersionRefused(t *testing.T) {
	doc := []byte(`{"schema_version":2,"provider_id":"wdtt"}`)
	if _, _, err := ParseMTProviderManifest(doc, now); !errors.Is(err, ErrUnknownSchemaVersion) {
		t.Fatalf("unknown version must be refused, got %v", err)
	}
}

func TestParseMT_ValidMapping(t *testing.T) {
	sha := strings.Repeat("a", 64)
	doc := []byte(`{
		"schema_version":1,
		"provider_id":"wdtt",
		"adapter_version":"0.1.0",
		"server_wire":"wdtt-specific",
		"server_class":"wdtt-compatible-exit",
		"architecture":"arm64",
		"capabilities":{"tcp":{"code":"BUILD_CONFIRMED","build":"PRESENT"},
		                "udp":{"code":"SOURCE_CONFIRMED","build":"PRESENT"}},
		"inbound_bind":"127.0.0.1:1081",
		"resource_requirements":{"admission_min_mb":512,"admission_source":"KAS-#137-combined-admission-policy","measured":true},
		"artifact_provenance":{"sha256":"` + sha + `","source_commit":"d39f9dad9163","artifact_path":"/opt/bin/worker"},
		"configuration_reference":"docs/profile-format.md"
	}`)
	m, f, err := ParseMTProviderManifest(doc, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Provider != "wdtt" || m.SocksBind != "127.0.0.1:1081" || m.Arch != "arm64" ||
		m.TCP != EvidenceBuildConfirmed || m.UDP != EvidenceSourceConfirmed ||
		m.ArtifactSHA256 != sha || m.MinMemMB != 512 {
		t.Fatalf("bad mapping: %+v", m)
	}
	// No silent field loss: every unmapped MT field must be reported.
	text := ""
	for _, x := range f {
		text += x.Code + ";"
	}
	for _, code := range []string{"MT_ADAPTER_VERSION", "MT_SERVER_WIRE", "MT_SERVER_CLASS", "MT_SOURCE_COMMIT", "MT_MEASURED_FLAG"} {
		if !strings.Contains(text, code) {
			t.Fatalf("missing warning %s in %s", code, text)
		}
	}
}

func TestParseMT_E2EDowngradedNotUpgraded(t *testing.T) {
	sha := strings.Repeat("a", 64)
	doc := []byte(`{
		"schema_version":1,"provider_id":"wdtt","adapter_version":"0.1.0",
		"server_wire":"wdtt","server_class":"x","architecture":"arm64",
		"capabilities":{"tcp":{"code":"E2E_VERIFIED","build":"PRESENT"},
		                "udp":{"code":"SOURCE_CONFIRMED","build":"PRESENT"}},
		"inbound_bind":"127.0.0.1:1081",
		"resource_requirements":{"admission_min_mb":null,"admission_source":"none","measured":false},
		"artifact_provenance":{"sha256":"` + sha + `","source_commit":"abcdef1234567","artifact_path":"/w"},
		"configuration_reference":"docs/x.md"
	}`)
	m, f, err := ParseMTProviderManifest(doc, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.TCP != EvidenceUnknown {
		t.Fatalf("unbacked MT E2E must downgrade to UNKNOWN, got %s", m.TCP)
	}
	if m.UDP != EvidenceSourceConfirmed {
		t.Fatalf("SOURCE_CONFIRMED must be carried verbatim, got %s", m.UDP)
	}
	found := false
	for _, x := range f {
		if x.Code == "MT_E2E_UNBACKED" {
			found = true
		}
	}
	if !found {
		t.Fatal("E2E downgrade must be reported")
	}
	// Null floor maps to 0 (no floor), never to a made-up number:
	if m.MinMemMB != 0 {
		t.Fatalf("null admission_min_mb must map to 0, got %d", m.MinMemMB)
	}
}
