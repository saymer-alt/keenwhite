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

// TestParseMT_PreFixShapeRejected guards the task-14 stage-A repair: the
// pre-fix converter accepted a WRONG MT-001 shape (architecture as a string,
// capabilities keyed tcp/udp with runtime evidence levels). That shape is
// not provider-v0.schema.json and must be refused now. The faithful shape is
// covered by mtfidelity_test.go against the original fixtures.
func TestParseMT_PreFixShapeRejected(t *testing.T) {
	sha := strings.Repeat("a", 64)
	oldShape := []byte(`{
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
	if _, _, err := ParseMTProviderManifest(oldShape, now); err == nil {
		t.Fatal("the pre-fix (string architecture) shape must be rejected: it is not MT-001")
	}
	// Runtime evidence levels were never valid MT-001 static levels either.
	noStatic := []byte(`{
		"schema_version":1,"provider_id":"wdtt","adapter_version":"0.1.0",
		"server_wire":"wdtt-specific","server_class":"wdtt-compatible-exit",
		"architecture":[{"arch":"aarch64","abi":"musl-static"}],
		"capabilities":{"socks_tcp":{"code":"E2E_VERIFIED","build":"PRESENT"}},
		"inbound_bind":"127.0.0.1:1081",
		"resource_requirements":{"admission_min_mb":null,"admission_source":null,"measured":false,"measurement_ref":null},
		"artifact_provenance":{"sha256":"` + sha + `","source_commit":"abcdef1234567","artifact_path":"/w"},
		"configuration_reference":"docs/x.md"
	}`)
	conv, err := ParseMTDocument(noStatic, now)
	if err != nil {
		t.Fatal(err)
	}
	if conv.Parsed || conv.Runnable {
		t.Fatal("E2E_VERIFIED is not an MT-001 static level and must not parse")
	}
	// Null floor still maps to 0 (no floor), never to a made-up number —
	// preserved behaviour from task #13.
	floor := mtMinimalDoc(t, func(w map[string]any) {
		w["resource_requirements"] = map[string]any{
			"admission_min_mb": nil, "admission_source": nil,
			"measured": false, "measurement_ref": nil,
		}
	})
	m, _, err := ParseMTProviderManifest(floor, now)
	if err != nil {
		t.Fatal(err)
	}
	if m.MinMemMB != 0 {
		t.Fatalf("null admission_min_mb must map to 0, got %d", m.MinMemMB)
	}
}
