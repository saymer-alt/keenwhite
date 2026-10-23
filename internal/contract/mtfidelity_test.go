package contract

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Task 14 stage A: the MT-001 converter must faithfully process the ORIGINAL
// provider-v0.schema.json fixtures (issue #4, reopened by independent audit).
// The fixtures live in the repository and are also validated against the real
// JSON Schema by tests/multitransport-contract.py — the Go mirror must agree
// with that verdict on the same corpus.

const mtFixtureDir = "../../docs/research/multitransport/fixtures"

func mtReadFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(mtFixtureDir, rel))
	if err != nil {
		t.Fatalf("fixture %s: %v", rel, err)
	}
	return b
}

func mtFindingsText(f []Finding) string {
	var b strings.Builder
	for _, x := range f {
		b.WriteString(string(x.Severity))
		b.WriteByte(':')
		b.WriteString(x.Code)
		b.WriteByte(' ')
	}
	return b.String()
}

func mtHasFinding(f []Finding, code string) bool {
	for _, x := range f {
		if x.Code == code {
			return true
		}
	}
	return false
}

// A1 regression: the original WDTT MT-001 fixture must parse, convert and
// stay faithful. Before the fix this failed with "cannot unmarshal array
// into Go struct field mtDoc.architecture of type string".
func TestMT001_OriginalWDTTFixture(t *testing.T) {
	conv, err := ParseMTDocument(mtReadFixture(t, "valid/wdtt-manifest.json"), now)
	if err != nil {
		t.Fatalf("original WDTT MT-001 fixture rejected: %v", err)
	}
	if !conv.Parsed || !conv.Runnable {
		t.Fatalf("WDTT fixture must parse and be runnable: parsed=%v runnable=%v findings=%s",
			conv.Parsed, conv.Runnable, mtFindingsText(conv.Findings))
	}
	d, m := conv.Document, conv.Map

	// Architecture array with verbatim arch+ABI, mapped through the ONLY
	// documented name mapping.
	if len(d.Architecture) != 1 || d.Architecture[0] != (MTArchitecture{Arch: "aarch64", ABI: "musl-static"}) {
		t.Fatalf("architecture not preserved: %+v", d.Architecture)
	}
	if m.Arch != "arm64" || m.ArchABI != "musl-static" {
		t.Fatalf("aarch64/musl-static must map to arm64 with verbatim ABI, got %s/%s", m.Arch, m.ArchABI)
	}

	// Static capabilities: code/build are two independent static attributes.
	if got := d.Capabilities["socks_tcp"]; got != (MTCapability{Code: "PRESENT", Build: "PRESENT"}) {
		t.Fatalf("socks_tcp not preserved: %+v", got)
	}
	// PRESENT/PRESENT maps to BUILD_CONFIRMED — the strongest static claim;
	// never to E2E_VERIFIED.
	if m.TCP != EvidenceBuildConfirmed {
		t.Fatalf("socks_tcp PRESENT/PRESENT must map to BUILD_CONFIRMED, got %s", m.TCP)
	}
	if m.UDP != EvidenceUnknown {
		t.Fatalf("socks_udp UNKNOWN/UNKNOWN must map to UNKNOWN, got %s", m.UDP)
	}
	if got := m.Capabilities["tun"]; got != (MTCapability{Code: "ABSENT", Build: "ABSENT"}) {
		t.Fatalf("tun must be preserved verbatim, got %+v", got)
	}

	// Metadata preservation (A4) — every field readable from the mapping.
	if m.AdapterVersion != "0.1.0" || m.ServerWire != "synthetic-wdtt-qwdtt-vkturn-dtls-wg-1" ||
		m.ServerClass != "synthetic-wdtt-exit-vps" || m.SourceCommit != "0123456789abcdef0123456789abcdef01234567" ||
		m.BuildRef != "synthetic-ci-run-0001" || m.ArtifactPath != "versions/0.1.0/worker" ||
		m.MinMemMB != 512 || m.KASCombinedRef != "KAS-#137-combined-admission-policy" ||
		m.ResourceNotes == "" || m.SocksBind != "127.0.0.1:1081" ||
		m.ProfilePath != "synthetic-docs/wdtt-owner-config-v0.md" {
		t.Fatalf("metadata lost: %+v", m)
	}
	if d.ResourceRequirements.Measured || d.ResourceRequirements.MeasurementRef != nil {
		t.Fatalf("resource flag not preserved (fixture states measured=false): %+v", d.ResourceRequirements)
	}

	// Difference ledger still reports what has no Provider v1 home. The
	// fixture states measured=false, so MT_MEASURED_FLAG must NOT appear.
	for _, code := range []string{"MT_ADAPTER_VERSION", "MT_SERVER_WIRE", "MT_SERVER_CLASS", "MT_SOURCE_COMMIT"} {
		if !mtHasFinding(conv.Findings, code) {
			t.Fatalf("missing difference report %s in %s", code, mtFindingsText(conv.Findings))
		}
	}
	if mtHasFinding(conv.Findings, "MT_MEASURED_FLAG") {
		t.Fatal("MT_MEASURED_FLAG must not fire for measured=false")
	}
	if !conv.Partial {
		t.Fatal("conversion with unmappable metadata must be marked Partial")
	}
}

// A1 regression: the original OpenFlux MT-001 fixture.
func TestMT001_OriginalOpenFluxFixture(t *testing.T) {
	conv, err := ParseMTDocument(mtReadFixture(t, "valid/openflux-manifest.json"), now)
	if err != nil {
		t.Fatalf("original OpenFlux MT-001 fixture rejected: %v", err)
	}
	if !conv.Parsed || !conv.Runnable {
		t.Fatalf("OpenFlux fixture must parse and be runnable: %s", mtFindingsText(conv.Findings))
	}
	m := conv.Map
	if m.Arch != "arm64" || m.ArchABI != "glibc-entware" {
		t.Fatalf("arch mapping wrong: %s/%s", m.Arch, m.ArchABI)
	}
	// PRESENT code with UNKNOWN build: sources confirm, build state unknown.
	if m.TCP != EvidenceSourceConfirmed || m.UDP != EvidenceSourceConfirmed {
		t.Fatalf("PRESENT/UNKNOWN must map to SOURCE_CONFIRMED, got tcp=%s udp=%s", m.TCP, m.UDP)
	}
	// null admission floor maps to 0 (no floor), never to an invented number.
	if m.MinMemMB != 0 || m.KASCombinedRef != "" {
		t.Fatalf("null floor must stay empty: %d/%q", m.MinMemMB, m.KASCombinedRef)
	}
	if !mtHasFinding(conv.Findings, "MT_ADAPTER_VERSION") {
		t.Fatal("difference report missing")
	}
}

// A2: several architectures of one provider are preserved and never
// resolved by an arbitrary choice.
func TestMT001_MultiArchitectureAmbiguityRefused(t *testing.T) {
	doc := mtMinimalDoc(t, func(w map[string]any) {
		w["architecture"] = []map[string]any{
			{"arch": "aarch64", "abi": "musl-static"},
			{"arch": "mipsel", "abi": "musl-static"},
		}
	})
	conv, err := ParseMTDocument(doc, now)
	if err != nil {
		t.Fatal(err)
	}
	// The document itself is schema-valid (the array is legal); only the
	// executable mapping is impossible.
	if !conv.Parsed {
		t.Fatalf("multi-architecture document is valid MT-001: %s", mtFindingsText(conv.Findings))
	}
	if conv.Runnable || conv.Map.Arch != "" {
		t.Fatalf("no architecture may be selected arbitrarily: arch=%q runnable=%v", conv.Map.Arch, conv.Runnable)
	}
	if !mtHasFinding(conv.Findings, "MT_ARCH_AMBIGUOUS") {
		t.Fatal("MT_ARCH_AMBIGUOUS finding required")
	}
	// Both entries remain in diagnostics (nothing pruned).
	if len(conv.Map.Architectures) != 2 {
		t.Fatalf("architectures must be preserved verbatim: %+v", conv.Map.Architectures)
	}
	// ParseMTProviderManifest (compatibility entry) refuses too.
	if _, _, err := ParseMTProviderManifest(doc, now); err == nil {
		t.Fatal("compatibility entry must refuse an ambiguous document")
	}
}

func TestMT001_MultiArchitectureSingleTarget(t *testing.T) {
	doc := mtMinimalDoc(t, func(w map[string]any) {
		w["architecture"] = []map[string]any{
			{"arch": "x86_64", "abi": "glibc"},
			{"arch": "mipsel", "abi": "musl-static"},
		}
	})
	conv, err := ParseMTDocument(doc, now)
	if err != nil {
		t.Fatal(err)
	}
	if !conv.Parsed {
		t.Fatalf("document is valid MT-001: %s", mtFindingsText(conv.Findings))
	}
	if conv.Map.Arch != "mipsle" {
		t.Fatalf("single mappable candidate must be selected, got %q", conv.Map.Arch)
	}
	if !mtHasFinding(conv.Findings, "MT_ARCH_UNMAPPED") {
		t.Fatal("x86_64 must be reported as unmapped")
	}
	if len(conv.Map.Architectures) != 2 || conv.Map.Architectures[0].Arch != "x86_64" {
		t.Fatalf("unmapped variants must survive in diagnostics: %+v", conv.Map.Architectures)
	}
}

// A3: the static-level mapping table — every combination, no promotion.
func TestMT001_StaticCapabilityMappingTable(t *testing.T) {
	cases := []struct {
		code, build, want string
		runnable          bool
	}{
		{"PRESENT", "PRESENT", EvidenceBuildConfirmed, true},
		{"PRESENT", "UNKNOWN", EvidenceSourceConfirmed, true},
		{"PRESENT", "ABSENT", EvidenceUnsupported, false},
		{"ABSENT", "ABSENT", EvidenceUnsupported, false},
		{"ABSENT", "UNKNOWN", EvidenceUnknown, true},
		{"UNKNOWN", "UNKNOWN", EvidenceUnknown, true},
		{"UNKNOWN", "ABSENT", EvidenceUnsupported, false},
	}
	for _, c := range cases {
		doc := mtMinimalDoc(t, func(w map[string]any) {
			w["capabilities"] = map[string]any{
				"socks_tcp": map[string]any{"code": c.code, "build": c.build},
				"socks_udp": map[string]any{"code": "PRESENT", "build": "PRESENT"},
			}
		})
		conv, err := ParseMTDocument(doc, now)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.code, c.build, err)
		}
		if conv.Map.TCP != c.want {
			t.Fatalf("code=%s build=%s: want %s, got %s", c.code, c.build, c.want, conv.Map.TCP)
		}
		if c.runnable != conv.Runnable {
			t.Fatalf("code=%s build=%s: runnable=%v, want %v", c.code, c.build, conv.Runnable, c.runnable)
		}
		if conv.Map.TCP == EvidenceE2EVerified {
			t.Fatal("static MT-001 data must never produce E2E_VERIFIED")
		}
		// The compatibility entry must agree with Runnable.
		_, _, err = ParseMTProviderManifest(doc, now)
		if (err == nil) != c.runnable {
			t.Fatalf("code=%s build=%s: compat entry err=%v want runnable=%v", c.code, c.build, err, c.runnable)
		}
	}
}

// A3: missing axis is UNKNOWN, never PASS, and blocks runnability.
func TestMT001_MissingAxisIsUnknownNotPASS(t *testing.T) {
	doc := mtMinimalDoc(t, func(w map[string]any) {
		w["capabilities"] = map[string]any{
			"socks_tcp": map[string]any{"code": "PRESENT", "build": "PRESENT"},
		}
	})
	conv, err := ParseMTDocument(doc, now)
	if err != nil {
		t.Fatal(err)
	}
	if conv.Runnable {
		t.Fatal("missing socks_udp axis must block a runnable manifest")
	}
	if conv.Map.UDP != EvidenceUnknown {
		t.Fatalf("missing axis must map to UNKNOWN, got %q", conv.Map.UDP)
	}
	if !mtHasFinding(conv.Findings, "MT_AXIS_MISSING") {
		t.Fatal("MT_AXIS_MISSING finding required")
	}
}

// A3: contradictory capabilities violate the schema invariant (and match the
// original invalid fixture).
func TestMT001_ContradictoryCapabilityRejected(t *testing.T) {
	doc := mtReadFixture(t, "invalid/contradictory-capability.json")
	conv, err := ParseMTDocument(doc, now)
	if err != nil {
		t.Fatalf("schema violations are findings, not parse refusals: %v", err)
	}
	if conv.Parsed || conv.Runnable {
		t.Fatalf("contradictory capability must not parse: %+v", conv)
	}
	if !mtHasFinding(conv.Findings, "MT_SCHEMA") {
		t.Fatal("schema finding required")
	}
	if _, _, err := ParseMTProviderManifest(doc, now); err == nil {
		t.Fatal("compatibility entry must refuse")
	}
}

// Schema parity on the ORIGINAL invalid fixture corpus (provider-v0 docs).
func TestMT001_InvalidFixtureParity(t *testing.T) {
	cases := []struct {
		fixture    string
		wantErr    error // non-nil = hard parse refusal
		wantSchema bool  // expect a MT_SCHEMA finding
	}{
		{"invalid/bad-capability-value.json", nil, true},
		{"invalid/bad-sha256.json", nil, true},
		{"invalid/missing-server-wire.json", nil, true},
		{"invalid/non-loopback-bind.json", nil, true},
		{"invalid/wrong-schema-version.json", ErrUnknownSchemaVersion, false},
	}
	for _, c := range cases {
		doc := mtReadFixture(t, c.fixture)
		conv, err := ParseMTDocument(doc, now)
		if c.wantErr != nil {
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("%s: want %v, got %v", c.fixture, c.wantErr, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected refusal: %v", c.fixture, err)
		}
		if conv.Parsed || conv.Runnable {
			t.Fatalf("%s: schema-invalid document must not parse", c.fixture)
		}
		if c.wantSchema && !mtHasFinding(conv.Findings, "MT_SCHEMA") {
			t.Fatalf("%s: MT_SCHEMA finding required, got %s", c.fixture, mtFindingsText(conv.Findings))
		}
		if _, _, err := ParseMTProviderManifest(doc, now); err == nil {
			t.Fatalf("%s: compatibility entry must refuse", c.fixture)
		}
	}
}

// A4: every MT-001 field survives the conversion — no silent loss.
func TestMT001_MetadataFullyPreserved(t *testing.T) {
	doc := mtMinimalDoc(t, func(w map[string]any) {
		w["adapter_version"] = "1.2.3-beta.1"
		w["server_wire"] = "custom-wire-v9"
		w["server_class"] = "custom-class-v9"
		w["resource_requirements"] = map[string]any{
			"admission_min_mb": 1024,
			"admission_source": "test-source-ref",
			"measured":         true,
			"measurement_ref":  "acc-record-0001",
			"notes":            "synthetic measurement note",
		}
		w["artifact_provenance"] = map[string]any{
			"sha256":        strings.Repeat("b", 64),
			"source_commit": "d39f9dad9163",
			"build_ref":     "ci-run-42",
			"artifact_path": "rel/path/worker",
		}
	})
	conv, err := ParseMTDocument(doc, now)
	if err != nil {
		t.Fatal(err)
	}
	if !conv.Parsed || !conv.Runnable {
		t.Fatalf("must be fully convertible: %s", mtFindingsText(conv.Findings))
	}
	m, d := conv.Map, conv.Document
	if m.AdapterVersion != "1.2.3-beta.1" || d.AdapterVersion != "1.2.3-beta.1" {
		t.Fatal("adapter_version lost")
	}
	if m.ServerWire != "custom-wire-v9" || m.ServerClass != "custom-class-v9" {
		t.Fatal("wire/class lost")
	}
	if m.SourceCommit != "d39f9dad9163" || m.BuildRef != "ci-run-42" || m.ArtifactPath != "rel/path/worker" {
		t.Fatal("provenance lost")
	}
	if m.MinMemMB != 1024 || m.KASCombinedRef != "test-source-ref" ||
		m.MeasurementRef != "acc-record-0001" || m.ResourceNotes != "synthetic measurement note" {
		t.Fatalf("resource metadata lost: %+v", m)
	}
	if !d.ResourceRequirements.Measured {
		t.Fatal("measured flag lost")
	}
}

// Unknown/unsupported schemaVersion stays a hard refusal (unchanged from
// task #13) — but via the shared parse path now.
func TestMT001_UnknownSchemaVersionStillRefused(t *testing.T) {
	if _, _, err := ParseMTProviderManifest([]byte(`{"schema_version":2,"provider_id":"wdtt"}`), now); !errors.Is(err, ErrUnknownSchemaVersion) {
		t.Fatalf("unknown version must be refused, got %v", err)
	}
}

// mtMinimalDoc returns a schema-valid MT-001 document with the WDTT fixture
// shape, overridden by mut.
func mtMinimalDoc(t *testing.T, mut func(w map[string]any)) []byte {
	t.Helper()
	w := map[string]any{
		"schema_version":  1,
		"provider_id":     "wdtt",
		"adapter_version": "0.1.0",
		"server_wire":     "synthetic-wdtt-qwdtt-vkturn-dtls-wg-1",
		"server_class":    "synthetic-wdtt-exit-vps",
		"architecture":    []map[string]any{{"arch": "aarch64", "abi": "musl-static"}},
		"capabilities": map[string]any{
			"socks_tcp": map[string]any{"code": "PRESENT", "build": "PRESENT"},
			"socks_udp": map[string]any{"code": "UNKNOWN", "build": "UNKNOWN"},
		},
		"inbound_bind": "127.0.0.1:1081",
		"resource_requirements": map[string]any{
			"admission_min_mb": 512,
			"admission_source": "KAS-#137-combined-admission-policy",
			"measured":         false,
			"measurement_ref":  nil,
		},
		"artifact_provenance": map[string]any{
			"sha256":        strings.Repeat("a", 64),
			"source_commit": "0123456789abcdef0123456789abcdef01234567",
			"artifact_path": "versions/0.1.0/worker",
		},
		"configuration_reference": "synthetic-docs/wdtt-owner-config-v0.md",
	}
	if mut != nil {
		mut(w)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
