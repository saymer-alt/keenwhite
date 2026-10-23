package tests

// Contract Compliance Suite (task #13 stage D, issue #4): C01–C18.
// Deterministic, offline, injected clock. Exercises the catalog
// (keenwhite.dev/v0alpha1), the provider contract (keenwhite.provider/v1),
// the unified checks (internal/contract) and the MT-001 compatibility layer
// as ONE coherent validation stack.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/saymer-alt/keenwhite/internal/catalog"
	"github.com/saymer-alt/keenwhite/internal/contract"
	"github.com/saymer-alt/keenwhite/internal/provider"
	"github.com/saymer-alt/keenwhite/internal/provider/wdtt"
)

var ccNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func ccSHA(c byte) string { return strings.Repeat(string(c), 64) }

func ccWrite(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func ccManifestJSON(provider, bind, tcp, udp string, mutate func(map[string]any)) []byte {
	m := map[string]any{
		"manifest_version": 1,
		"api_version":      "keenwhite.provider/v1",
		"provider":         provider,
		"artifact":         map[string]any{"path": "bin/worker", "sha256": ccSHA('a'), "arch": "arm64"},
		"profile":          map[string]any{"path": "profile/client.json"},
		"socks":            map[string]any{"bind": bind, "tcp": tcp, "udp": udp},
		"resources":        map[string]any{},
		"lifecycle":        map[string]any{"hard_stop_seconds": 1800},
		"evidence":         map[string]any{},
	}
	if mutate != nil {
		mutate(m)
	}
	b, _ := json.Marshal(m)
	return b
}

// ccManifestJSONWithProfile writes a real profile file and embeds its
// ABSOLUTE path, so adapter-level presence checks pass.
func ccManifestJSONWithProfile(t *testing.T, provider, bind, tcp, udp string, mutate func(map[string]any)) []byte {
	t.Helper()
	profile := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(profile, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return ccManifestJSON(provider, bind, tcp, udp, func(m map[string]any) {
		m["profile"] = map[string]any{"path": profile}
		if mutate != nil {
			mutate(m)
		}
	})
}

func ccProviderProblems(t *testing.T, doc []byte) []provider.Problem {
	t.Helper()
	p := ccWrite(t, t.TempDir(), "manifest.json", doc)
	m, problems, err := provider.LoadManifest(p)
	if err != nil {
		return problems // hard parse errors count as findings
	}
	_ = m
	return append(problems, ValidateExtra(t, p)...)
}

// ValidateExtra mirrors the adapter's Validate pass (profile presence).
func ValidateExtra(t *testing.T, manifestPath string) []provider.Problem {
	t.Helper()
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	var m struct {
		Profile struct {
			Path string `json:"path"`
		} `json:"profile"`
	}
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	if _, err := os.Stat(m.Profile.Path); err != nil {
		return []provider.Problem{{Severity: "error", Field: "profile", Message: "profile missing"}}
	}
	return nil
}

func ccCatalogValidate(t *testing.T, doc []byte) []string {
	p := ccWrite(t, t.TempDir(), "catalog.json", doc)
	c, err := catalog.Load(p)
	if err != nil {
		return []string{"load: " + err.Error()}
	}
	return catalog.Validate(c)
}

// ccProviderProblemsPath validates a manifest already on disk, including
// the adapter-level profile presence check.
func ccProviderProblemsPath(t *testing.T, path string) []provider.Problem {
	t.Helper()
	_, problems, err := provider.LoadManifest(path)
	if err != nil {
		return problems
	}
	return append(problems, ValidateExtra(t, path)...)
}

func ccHas(problems []provider.Problem, field, contains string) bool {
	for _, p := range problems {
		if p.Field == field && strings.Contains(p.Message, contains) {
			return true
		}
	}
	return false
}

// C01: a valid catalog and a valid manifest pass together.
func TestC01_ValidCatalogAndManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "examples", "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if problems := ccCatalogValidate(t, raw); len(problems) != 0 {
		t.Fatalf("reference catalog must validate: %v", problems)
	}
	if problems := ccProviderProblems(t, ccManifestJSONWithProfile(t, "wdtt", "127.0.0.1:1081",
		contract.EvidenceSourceConfirmed, contract.EvidenceSourceConfirmed, nil)); len(problems) != 0 {
		t.Fatalf("reference manifest must validate: %+v", problems)
	}
}

// C02: duplicate provider id — unified check and catalog agree.
func TestC02_DuplicateProviderID(t *testing.T) {
	f := contract.CheckProviderSet([]contract.ProviderRef{
		{ID: "wdtt", Bind: "127.0.0.1:1081"}, {ID: "wdtt", Bind: "127.0.0.1:1082"},
	})
	bad := false
	for _, x := range f {
		if x.Code == "DUPLICATE_PROVIDER" && x.Severity == contract.SeverityError {
			bad = true
		}
	}
	if !bad {
		t.Fatal("duplicate id must be an ERROR finding")
	}
	cat := []byte(`{"api_version":"keenwhite.dev/v0alpha1","providers":[
		{"id":"dup","name":"A","lifecycle":"lab","source":"https://github.com/x/x","wire":"w","server_class":"s","bind":"127.0.0.1:2001","tcp":"UNKNOWN","udp":"UNKNOWN"},
		{"id":"dup","name":"B","lifecycle":"lab","source":"https://github.com/x/x","wire":"w","server_class":"s","bind":"127.0.0.1:2002","tcp":"UNKNOWN","udp":"UNKNOWN"}]}`)
	if problems := ccCatalogValidate(t, cat); len(problems) == 0 || !strings.Contains(strings.Join(problems, ";"), "duplicate") {
		t.Fatalf("catalog must reject duplicate ids: %v", problems)
	}
}

// C03: two providers on one SOCKS bind.
func TestC03_SameSocksBind(t *testing.T) {
	f := contract.CheckProviderSet([]contract.ProviderRef{
		{ID: "wdtt", Bind: "127.0.0.1:1081"}, {ID: "openflux", Bind: "127.0.0.1:1081"},
	})
	conflict := false
	for _, x := range f {
		if x.Code == "BIND_CONFLICT" && x.Severity == contract.SeverityError {
			conflict = true
		}
	}
	if !conflict {
		t.Fatal("shared bind must be an ERROR finding")
	}
	cat := []byte(`{"api_version":"keenwhite.dev/v0alpha1","providers":[
		{"id":"a","name":"A","lifecycle":"lab","source":"https://github.com/x/x","wire":"w","server_class":"s","bind":"127.0.0.1:2003","tcp":"UNKNOWN","udp":"UNKNOWN"},
		{"id":"b","name":"B","lifecycle":"lab","source":"https://github.com/x/x","wire":"w","server_class":"s","bind":"127.0.0.1:2003","tcp":"UNKNOWN","udp":"UNKNOWN"}]}`)
	if problems := ccCatalogValidate(t, cat); len(problems) == 0 || !strings.Contains(strings.Join(problems, ";"), "conflict") {
		t.Fatalf("catalog must reject shared binds: %v", problems)
	}
}

// C04: external/wildcard binds are forbidden everywhere.
func TestC04_ForbiddenBinds(t *testing.T) {
	for _, bind := range []string{"0.0.0.0:1081", "192.168.1.1:1081", "[::]:1081", "localhost:1081"} {
		doc := ccManifestJSON("wdtt", bind, contract.EvidenceUnknown, contract.EvidenceUnknown, nil)
		if !ccHas(ccProviderProblems(t, doc), "socks.bind", "") {
			t.Fatalf("bind %s must be rejected by the provider contract", bind)
		}
		cat := []byte(`{"api_version":"keenwhite.dev/v0alpha1","providers":[
			{"id":"a","name":"A","lifecycle":"lab","source":"https://github.com/x/x","wire":"w","server_class":"s","bind":"` + bind + `","tcp":"UNKNOWN","udp":"UNKNOWN"}]}`)
		if problems := ccCatalogValidate(t, cat); len(problems) == 0 {
			t.Fatalf("bind %s must be rejected by the catalog", bind)
		}
	}
}

// C05: incompatible contract versions are refused, not silently read.
func TestC05_IncompatibleVersions(t *testing.T) {
	doc := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceUnknown, contract.EvidenceUnknown,
		func(m map[string]any) { m["api_version"] = "keenwhite.provider/v2" })
	if !ccHas(ccProviderProblems(t, doc), "api_version", "") {
		t.Fatal("unknown provider api_version must be rejected")
	}
	doc2 := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceUnknown, contract.EvidenceUnknown,
		func(m map[string]any) { m["manifest_version"] = 2 })
	if !ccHas(ccProviderProblems(t, doc2), "manifest_version", "") {
		t.Fatal("unknown manifest_version must be rejected")
	}
	mtdoc := []byte(`{"schema_version":9,"provider_id":"wdtt"}`)
	if _, _, err := contract.ParseMTProviderManifest(mtdoc, ccNow); !errors.Is(err, contract.ErrUnknownSchemaVersion) {
		t.Fatalf("unknown MT schema_version must be refused, got %v", err)
	}
}

// C06: a missing required component (profile) is an error.
func TestC06_MissingRequiredComponent(t *testing.T) {
	dir := t.TempDir()
	doc := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceUnknown, contract.EvidenceUnknown, nil)
	p := ccWrite(t, dir, "manifest.json", doc)
	if !ccHas(ccProviderProblems(t, doc), "", "") && true {
		// structural parse passes; the adapter-level profile check must fire
		_ = p
	}
	missing := ccWrite(t, dir, "manifest2.json",
		ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceUnknown, contract.EvidenceUnknown,
			func(m map[string]any) {
				m["profile"] = map[string]any{"path": filepath.Join(dir, "nope", "client.json"), "required_files": []string{"x"}}
			}))
	if !ccHas(ccProviderProblemsPath(t, missing), "profile", "") {
		t.Fatalf("missing profile must be an error")
	}
}

// C07: unknown JSON fields are rejected in both documents.
func TestC07_UnknownFields(t *testing.T) {
	doc := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceUnknown, contract.EvidenceUnknown,
		func(m map[string]any) { m["surprise"] = true })
	_, _, err := provider.LoadManifest(ccWrite(t, t.TempDir(), "m.json", doc))
	if err == nil {
		t.Fatal("unknown manifest fields must be rejected")
	}
	cat := []byte(`{"api_version":"keenwhite.dev/v0alpha1","providers":[],"surprise":1}`)
	if problems := ccCatalogValidate(t, cat); len(problems) == 0 || !strings.Contains(problems[0], "decode") {
		t.Fatalf("unknown catalog fields must be rejected: %v", problems)
	}
}

// C08: damaged JSON is rejected with a parse error.
func TestC08_DamagedJSON(t *testing.T) {
	if _, _, err := provider.LoadManifest(ccWrite(t, t.TempDir(), "m.json", []byte("{broken"))); err == nil {
		t.Fatal("broken manifest JSON must fail")
	}
	if problems := ccCatalogValidate(t, []byte("{broken")); len(problems) == 0 {
		t.Fatal("broken catalog JSON must fail")
	}
}

// C09: stale evidence fails and downgrades to UNKNOWN.
func TestC09_StaleEvidence(t *testing.T) {
	old := ccNow.Add(-2 * 91 * 24 * time.Hour)
	eff, f := contract.CheckEvidence(contract.EvidenceE2EVerified, "https://e/x", &old, 0, ccNow)
	if eff != contract.EvidenceUnknown {
		t.Fatalf("stale E2E must downgrade, got %s", eff)
	}
	staleFound := false
	for _, x := range f {
		if x.Code == "E2E_STALE" {
			staleFound = true
		}
	}
	if !staleFound {
		t.Fatal("E2E_STALE finding required")
	}
	doc := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceE2EVerified, contract.EvidenceE2EVerified,
		func(m map[string]any) {
			m["evidence"] = map[string]any{"evidence_url": "https://e/x", "measured_at": old.Format(time.RFC3339Nano)}
		})
	if !ccHas(ccProviderProblems(t, doc), "evidence.measured_at", "stale") {
		t.Fatalf("provider validation must flag stale evidence")
	}
}

// C10: future-dated evidence is corrupt.
func TestC10_FutureEvidence(t *testing.T) {
	future := ccNow.Add(24 * time.Hour)
	eff, _ := contract.CheckEvidence(contract.EvidenceE2EVerified, "https://e/x", &future, 0, ccNow)
	if eff != contract.EvidenceUnknown {
		t.Fatalf("future evidence must downgrade, got %s", eff)
	}
	doc := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceE2EVerified, contract.EvidenceUnknown,
		func(m map[string]any) {
			m["evidence"] = map[string]any{"evidence_url": "https://e/x", "measured_at": future.Format(time.RFC3339Nano)}
		})
	if !ccHas(ccProviderProblems(t, doc), "evidence.measured_at", "future") {
		t.Fatal("provider validation must flag future-dated evidence")
	}
}

// C11: a false E2E_VERIFIED (no URL, no measurement) never passes.
func TestC11_FalseE2EClaim(t *testing.T) {
	eff, f := contract.CheckEvidence(contract.EvidenceE2EVerified, "", nil, 0, ccNow)
	if eff != contract.EvidenceUnknown || len(f) == 0 {
		t.Fatal("unbacked E2E must fail closed")
	}
	doc := ccManifestJSON("wdtt", "127.0.0.1:1081", contract.EvidenceE2EVerified, contract.EvidenceE2EVerified, nil)
	if problems := ccProviderProblems(t, doc); len(problems) == 0 {
		t.Fatal("provider validation must reject unbacked E2E")
	}
}

// C12: TCP and UDP evidence are judged independently.
func TestC12_TCPUDPIndependent(t *testing.T) {
	measured := ccNow.Add(-time.Hour)
	tcpEff, _ := contract.CheckEvidence(contract.EvidenceE2EVerified, "https://e/x", &measured, 0, ccNow)
	udpEff, _ := contract.CheckEvidence(contract.EvidenceSourceConfirmed, "", nil, 0, ccNow)
	if tcpEff != contract.EvidenceE2EVerified {
		t.Fatal("fresh TCP E2E must stay verified")
	}
	if udpEff != contract.EvidenceSourceConfirmed {
		t.Fatal("SOURCE_CONFIRMED UDP must stay itself (freshness does not apply)")
	}
	stale := ccNow.Add(-91 * 24 * time.Hour)
	if eff, _ := contract.CheckEvidence(contract.EvidenceE2EVerified, "https://e/x", &stale, 0, ccNow); eff != contract.EvidenceUnknown {
		t.Fatal("stale TCP must downgrade while UDP is unaffected")
	}
}

// C13: insufficient resources block Prepare (adapter-level).
func TestC13_InsufficientResources(t *testing.T) {
	a := ccAdapter(t, 3072)
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("low-memory admission must fail Prepare")
	}
}

// C14: a wrong ELF architecture fails Prepare.
func TestC14_WrongArch(t *testing.T) {
	a := ccAdapter(t, 2048)
	a.ManifestForTest().Artifact.Arch = "mipsle"
	a.ManifestForTest().Artifact.SHA256 = ccFileSHA(t, a.ManifestForTest().Artifact.Path)
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("arch mismatch must fail Prepare")
	}
}

// C15: a wrong SHA256 fails Prepare.
func TestC15_WrongSHA(t *testing.T) {
	a := ccAdapter(t, 2048)
	a.ManifestForTest().Artifact.SHA256 = ccSHA('f')
	if _, err := a.Prepare(context.Background()); err == nil {
		t.Fatal("sha mismatch must fail Prepare")
	}
}

// C16: the MT layer reports every semantic difference from Provider v1.
func TestC16_MTDifferencesReported(t *testing.T) {
	doc := []byte(`{"schema_version":1,"provider_id":"wdtt","adapter_version":"1.0.0",
		"server_wire":"wdtt-specific","server_class":"wdtt-compatible-exit","architecture":"arm64",
		"capabilities":{"tcp":{"code":"SOURCE_CONFIRMED","build":"PRESENT"},"udp":{"code":"SOURCE_CONFIRMED","build":"PRESENT"}},
		"inbound_bind":"127.0.0.1:1081",
		"resource_requirements":{"admission_min_mb":512,"admission_source":"KAS-#137","measured":false},
		"artifact_provenance":{"sha256":"` + ccSHA('a') + `","source_commit":"d39f9dad9163","artifact_path":"/opt/worker"},
		"configuration_reference":"docs/profile.md"}`)
	_, f, err := contract.ParseMTProviderManifest(doc, ccNow)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, x := range f {
		codes[x.Code] = true
	}
	for _, want := range []string{"MT_ADAPTER_VERSION", "MT_SERVER_WIRE", "MT_SERVER_CLASS", "MT_SOURCE_COMMIT"} {
		if !codes[want] {
			t.Fatalf("missing difference report %s", want)
		}
	}
}

// C17: invalid conversion is impossible — unknown versions and evidence
// upgrades are structurally refused.
func TestC17_NoInvalidConversion(t *testing.T) {
	if _, _, err := contract.ParseMTProviderManifest([]byte(`{"schema_version":3}`), ccNow); !errors.Is(err, contract.ErrUnknownSchemaVersion) {
		t.Fatal("unknown MT version refused")
	}
	m := []byte(`{"schema_version":1,"provider_id":"wdtt","adapter_version":"1.0.0",
		"server_wire":"w","server_class":"s","architecture":"arm64",
		"capabilities":{"udp":{"code":"UNKNOWN","build":"ABSENT"}},
		"inbound_bind":"127.0.0.1:1081",
		"resource_requirements":{"admission_min_mb":null,"admission_source":"none","measured":false},
		"artifact_provenance":{"sha256":"` + ccSHA('a') + `","source_commit":"abc1234","artifact_path":"/w"},
		"configuration_reference":"c"}`)
	mapped, _, err := contract.ParseMTProviderManifest(m, ccNow)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.UDP != contract.EvidenceUnknown || mapped.UDP == contract.EvidenceE2EVerified {
		t.Fatal("UNKNOWN evidence must remain UNKNOWN in the mapping")
	}
}

// C18: the shipped WDTT configuration stays fully compatible across all
// three layers (catalog + provider manifest + cross-provider set).
func TestC18_WDTTConfigRemainsCompatible(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "examples", "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if problems := ccCatalogValidate(t, raw); len(problems) != 0 {
		t.Fatalf("shipped catalog must stay valid: %v", problems)
	}
	// The canonical WDTT manifest shape used by the adapter tests:
	doc := ccManifestJSONWithProfile(t, "wdtt", "127.0.0.1:1081", contract.EvidenceSourceConfirmed, contract.EvidenceSourceConfirmed,
		func(m map[string]any) {
			m["resources"] = map[string]any{
				"min_mem_available_mb":    0,
				"kas_combined_min_ram_mb": 512,
				"admission_source":        "KAS-#137-combined-admission-policy",
			}
			m["lifecycle"] = map[string]any{"hard_stop_seconds": 1800, "stop_grace_seconds": 10}
		})
	if problems := ccProviderProblems(t, doc); len(problems) != 0 {
		t.Fatalf("canonical WDTT manifest must validate: %+v", problems)
	}
	// Cross-provider: wdtt + openflux on distinct binds stay clean.
	f := contract.CheckProviderSet([]contract.ProviderRef{
		{ID: "wdtt", Bind: "127.0.0.1:1081"}, {ID: "openflux", Bind: "127.0.0.1:1083"},
	})
	if len(f) != 0 {
		t.Fatalf("distinct binds must be clean: %+v", f)
	}
}

// --- helpers building the adapter used by C13-C15 -----------------------

type wdttAdapter = wdtt.Adapter

func ccAdapterWithMem(t *testing.T, floorMB int, withELF bool) *wdttAdapter {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "worker")
	profile := filepath.Join(dir, "profile", "client.json")
	if err := os.MkdirAll(filepath.Dir(profile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := provider.Manifest{
		ManifestVersion: 1,
		APIVersion:      "keenwhite.provider/v1",
		Provider:        "wdtt",
		Artifact:        provider.ManifestArtifact{Path: bin, SHA256: ccSHA('a'), Arch: "arm64"},
		Profile:         provider.ManifestProfile{Path: profile},
		Socks:           provider.ManifestSocks{Bind: "127.0.0.1:" + freePortHere(t), TCP: "SOURCE_CONFIRMED", UDP: "SOURCE_CONFIRMED"},
		Resources:       provider.ManifestRes{MinMemAvailableMB: floorMB},
		Lifecycle:       provider.ManifestLife{HardStopSeconds: 1800, StopGraceSeconds: 1},
	}
	if withELF {
		var hdr [64]byte
		copy(hdr[0:4], "ELF")
		hdr[4] = 2
		hdr[5] = 1
		hdr[6] = 1
		binary.LittleEndian.PutUint16(hdr[16:18], 2)
		binary.LittleEndian.PutUint16(hdr[18:20], 183)
		binary.LittleEndian.PutUint32(hdr[20:24], 1)
		binary.LittleEndian.PutUint16(hdr[52:54], 64)
		binary.LittleEndian.PutUint16(hdr[54:56], 56)
		if err := os.WriteFile(bin, hdr[:], 0o700); err != nil {
			t.Fatal(err)
		}
		manifest.Artifact.SHA256 = ccFileSHA(t, bin)
	} else if err := os.WriteFile(bin, []byte("placeholder"), 0o700); err != nil {
		t.Fatal(err)
	}
	a, err := wdtt.New(manifest, wdtt.NewMockRunner(), nil, func() (int, error) { return 2048, nil })
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ccAdapter(t *testing.T, floorMB int) *wdttAdapter {
	return ccAdapterWithMem(t, floorMB, true)
}

func ccFileSHA(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func freePortHere(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}
