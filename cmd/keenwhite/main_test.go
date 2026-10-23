package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CLI-level tests for issue #32: they drive the real run() entrypoint and
// assert exit codes, stdout and stderr semantics — WARNING must not fail,
// ERROR must fail, and unverifiable E2E claims must never be echoed as
// verified in any output mode.

type cliResult struct {
	code   int
	stdout string
	stderr string
}

func cliRun(t *testing.T, args ...string) cliResult {
	t.Helper()
	var out, errBuf bytes.Buffer
	oldOut, oldErr := stdout, stderr
	stdout, stderr = &out, &errBuf
	defer func() { stdout, stderr = oldOut, oldErr }()
	code := run(args)
	return cliResult{code, out.String(), errBuf.String()}
}

func cliCatalog(t *testing.T, providersJSON string) string {
	t.Helper()
	dir := t.TempDir()
	doc := fmt.Sprintf(`{"api_version":"keenwhite.dev/v0alpha1","providers":[%s]}`, providersJSON)
	p := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// One structurally valid provider with customisable tcp/udp/evidence fields.
type cliProvider struct {
	id      string
	bind    string
	tcp     string
	udp     string
	url     string
	dateISO string // "" = absent
}

func cliProviderJSON(p cliProvider) string {
	url := ""
	if p.url != "" {
		url = fmt.Sprintf(`,"evidence_url":%q`, p.url)
	}
	date := ""
	if p.dateISO != "" {
		date = fmt.Sprintf(`,"evidence_measured_at":%q`, p.dateISO)
	}
	return fmt.Sprintf(`{"id":%q,"name":"P %s","lifecycle":"lab","source":"https://github.com/example/p","wire":"wire-a","server_class":"class-a","bind":%q,"tcp":%q,"udp":%q%s%s}`,
		p.id, p.id, p.bind, p.tcp, p.udp, url, date)
}

func rfc(rfcOffset time.Duration) string {
	return time.Now().Add(rfcOffset).UTC().Format(time.RFC3339Nano)
}

func TestCLI_ValidateClean(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "SOURCE_CONFIRMED", "UNKNOWN", "", ""}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "PASS") || strings.Contains(r.stdout, "WARNING") {
		t.Fatalf("clean catalog must PASS without warnings: %s", r.stdout)
	}
}

func TestCLI_ValidateLegacyE2EWithoutDate(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "UNKNOWN", "https://e.example/x", ""}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("legacy warning must not fail: exit=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "WARNING [E2E_NO_MEASUREMENT]") || !strings.Contains(r.stdout, "UNKNOWN") {
		t.Fatalf("downgrade warning not visible: %s", r.stdout)
	}
}

func TestCLI_ValidateE2EWithoutURL(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "UNKNOWN", "", rfc(-time.Hour)}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("advisory downgrade must not fail: exit=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "WARNING [E2E_NO_URL]") {
		t.Fatalf("E2E_NO_URL warning not visible: %s", r.stdout)
	}
}

func TestCLI_ValidateStaleE2E(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "UNKNOWN", "https://e.example/x", rfc(-100 * 24 * time.Hour)}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("stale E2E must downgrade, not fail: exit=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "WARNING [E2E_STALE]") {
		t.Fatalf("E2E_STALE warning not visible: %s", r.stdout)
	}
}

func TestCLI_ValidateFutureE2E(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "UNKNOWN", "https://e.example/x", rfc(48 * time.Hour)}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("future E2E must downgrade, not fail: exit=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "WARNING [E2E_FUTURE_DATE]") {
		t.Fatalf("E2E_FUTURE_DATE warning not visible: %s", r.stdout)
	}
}

func TestCLI_ValidateFreshE2EStaysVerified(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "SOURCE_CONFIRMED", "https://e.example/x", rfc(-24 * time.Hour)}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("fresh E2E must pass: exit=%d stderr=%s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, "WARNING") {
		t.Fatalf("fresh E2E must not warn: %s", r.stdout)
	}
}

func TestCLI_MixedAxesIndependence(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "E2E_VERIFIED", "https://e.example/x", rfc(-100 * 24 * time.Hour)}))
	r := cliRun(t, "providers", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.code, r.stderr)
	}
	// TCP judged independently from UDP: BOTH downgraded here, each must be
	// reported at its own effective level (never the written E2E_VERIFIED).
	if !strings.Contains(r.stdout, "TCP:UNKNOWN UDP:UNKNOWN") {
		t.Fatalf("effective levels not shown: %s", r.stdout)
	}
	if !strings.Contains(r.stdout, "effective level") {
		t.Fatalf("downgrade note missing: %s", r.stdout)
	}
	v := cliRun(t, "validate", "-catalog", p)
	if !strings.Contains(v.stdout, "WARNING [E2E_STALE]") || strings.Count(v.stdout, "WARNING [E2E_STALE]") != 2 {
		t.Fatalf("each axis must be reported separately: %s", v.stdout)
	}
}

func TestCLI_TCPStaleUDPFresh(t *testing.T) {
	// Two providers share nothing: one stale-TCP, one fresh-TCP — effective
	// levels must differ per provider and per axis.
	p := cliCatalog(t,
		cliProviderJSON(cliProvider{"prov-a", "127.0.0.1:1081", "E2E_VERIFIED", "SOURCE_CONFIRMED", "https://e.example/x", rfc(-100 * 24 * time.Hour)})+","+
			cliProviderJSON(cliProvider{"prov-b", "127.0.0.1:1083", "E2E_VERIFIED", "SOURCE_CONFIRMED", "https://e.example/y", rfc(-24 * time.Hour)}))
	r := cliRun(t, "providers", "-catalog", p)
	if r.code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "TCP:UNKNOWN") || !strings.Contains(r.stdout, "TCP:E2E_VERIFIED") {
		t.Fatalf("TCP staleness must be per-provider: %s", r.stdout)
	}
}

func TestCLI_ValidateErrorExitCode(t *testing.T) {
	p := cliCatalog(t,
		cliProviderJSON(cliProvider{"a", "127.0.0.1:1081", "SOURCE_CONFIRMED", "UNKNOWN", "", ""})+","+
			cliProviderJSON(cliProvider{"b", "127.0.0.1:1081", "SOURCE_CONFIRMED", "UNKNOWN", "", ""}))
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 1 {
		t.Fatalf("bind conflict must exit 1, got %d", r.code)
	}
	if !strings.Contains(r.stderr, "FAIL: [BIND_CONFLICT]") {
		t.Fatalf("structured bind-conflict error not shown: %s", r.stderr)
	}
}

func TestCLI_BrokenJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(p, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 1 {
		t.Fatalf("broken JSON must exit 1, got %d", r.code)
	}
}

func TestCLI_UnknownFieldRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cat.json")
	doc := `{"api_version":"keenwhite.dev/v0alpha1","providers":[],"surprise":1}`
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 1 {
		t.Fatalf("unknown fields must exit 1, got %d", r.code)
	}
}

func TestCLI_ErrorPlusWarning(t *testing.T) {
	// Provider a: structural ERROR (invalid lifecycle). Provider b: legacy
	// E2E warning. ERROR must win the exit code; PASS must not be printed.
	p := cliCatalog(t,
		cliProviderJSON(cliProvider{"a", "127.0.0.1:1081", "SOURCE_CONFIRMED", "UNKNOWN", "", ""})+","+
			cliProviderJSON(cliProvider{"b", "127.0.0.1:1083", "E2E_VERIFIED", "UNKNOWN", "https://e.example/y", ""}))
	// make provider a structurally invalid by rewriting its lifecycle
	docBytes, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(docBytes), `"id":"a","name":"P a","lifecycle":"lab"`, `"id":"a","name":"P a","lifecycle":"production"`, 1)
	if err := os.WriteFile(p, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 1 {
		t.Fatalf("ERROR must exit 1, got %d", r.code)
	}
	if !strings.Contains(r.stderr, "FAIL: [LIFECYCLE]") {
		t.Fatalf("lifecycle error not shown: %s", r.stderr)
	}
	if strings.Contains(r.stdout, "PASS") {
		t.Fatalf("PASS must not be printed for an invalid catalog: %s", r.stdout)
	}
}

func TestCLI_EmptyProvidersFails(t *testing.T) {
	p := cliCatalog(t, "")
	r := cliRun(t, "validate", "-catalog", p)
	if r.code != 1 || !strings.Contains(r.stderr, "FAIL: [EMPTY_CATALOG]") {
		t.Fatalf("empty catalog must fail structurally: code=%d stderr=%s", r.code, r.stderr)
	}
}

func TestCLI_DoctorJSONTextEquivalence(t *testing.T) {
	p := cliCatalog(t, cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "E2E_VERIFIED", "SOURCE_CONFIRMED", "https://e.example/x", rfc(-100 * 24 * time.Hour)}))
	j := cliRun(t, "doctor", "-catalog", p, "-json")
	x := cliRun(t, "doctor", "-catalog", p)
	if j.code != 0 || x.code != 0 {
		t.Fatalf("codes: json=%d text=%d", j.code, x.code)
	}
	var doc struct {
		Tool    string `json:"tool"`
		Mode    string `json:"mode"`
		Results []struct {
			Provider string `json:"provider"`
			TCPE2E   string `json:"tcp_e2e"`
			UDPE2E   string `json:"udp_e2e"`
		} `json:"results"`
		Warnings []struct {
			Severity string `json:"severity"`
			Code     string `json:"code"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(j.stdout), &doc); err != nil {
		t.Fatalf("doctor -json must be valid JSON: %v\n%s", err, j.stdout)
	}
	if len(doc.Results) != 1 || doc.Results[0].TCPE2E != "UNKNOWN" || doc.Results[0].UDPE2E != "SOURCE_CONFIRMED" {
		t.Fatalf("JSON must carry effective levels: %+v", doc.Results)
	}
	if len(doc.Warnings) == 0 || doc.Warnings[0].Code != "E2E_STALE" || doc.Warnings[0].Severity != "WARNING" {
		t.Fatalf("JSON must carry the warning list: %+v", doc.Warnings)
	}
	// Text mode: same effective level and same warning code, visible.
	if !strings.Contains(x.stdout, "TCP_E2E=UNKNOWN") || !strings.Contains(x.stdout, "UDP_E2E=SOURCE_CONFIRMED") {
		t.Fatalf("text doctor must show effective levels: %s", x.stdout)
	}
	if !strings.Contains(x.stdout, "WARNING [E2E_STALE]") {
		t.Fatalf("text doctor must show the same warnings: %s", x.stdout)
	}
}

func TestCLI_OldCatalogBehaviourUnchanged(t *testing.T) {
	// A pre-0.2.1-style catalog without any E2E claim must behave exactly as
	// before: PASS without warnings, written levels shown as-is.
	p := cliCatalog(t,
		cliProviderJSON(cliProvider{"wdtt", "127.0.0.1:1081", "SOURCE_CONFIRMED", "BUILD_CONFIRMED", "", ""})+","+
			cliProviderJSON(cliProvider{"openflux", "127.0.0.1:1083", "BUILD_CONFIRMED", "SOURCE_CONFIRMED", "", ""}))
	v := cliRun(t, "validate", "-catalog", p)
	if v.code != 0 || strings.Contains(v.stdout, "WARNING") {
		t.Fatalf("legacy catalog must pass without warnings: %+v", v)
	}
	pr := cliRun(t, "providers", "-catalog", p)
	if pr.code != 0 || strings.Contains(pr.stdout, "note:") {
		t.Fatalf("no downgrade notes expected: %s", pr.stdout)
	}
	if !strings.Contains(pr.stdout, "TCP:SOURCE_CONFIRMED UDP:BUILD_CONFIRMED") {
		t.Fatalf("written levels must be shown unchanged: %s", pr.stdout)
	}
	d := cliRun(t, "doctor", "-catalog", p)
	if d.code != 0 || !strings.Contains(d.stdout, "TCP_E2E=SOURCE_CONFIRMED") {
		t.Fatalf("doctor must show written levels: %s", d.stdout)
	}
}

func TestCLI_UnknownCommand(t *testing.T) {
	r := cliRun(t, "frobnicate")
	if r.code != 2 {
		t.Fatalf("unknown command must exit 2, got %d", r.code)
	}
}
