package acceptance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/saymer-alt/keenwhite/internal/contract"
)

var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func rec(mut func(*Record)) Record {
	r := Record{
		RecordVersion:   1,
		ProviderID:      "wdtt",
		ArtifactSHA256:  strings.Repeat("a", 64),
		Arch:            "arm64",
		ABI:             "musl-static",
		ResultSource:    SourceLocalFake,
		EnvironmentID:   "synthetic-harness-01",
		ProfileLabel:    "synthetic-18x1-short",
		ContractVersion: "keenwhite.provider/v1",
		TestType:        TestTCPE2E,
		MeasuredAt:      now.Add(-time.Hour),
		Result:          ResultPASS,
	}
	if mut != nil {
		mut(&r)
	}
	return r
}

func validOrFatal(t *testing.T, r Record) {
	t.Helper()
	if f := ValidateRecord(r, now); len(f) != 0 {
		t.Fatalf("fixture record must be valid: %+v", f)
	}
}

func TestRoundtrip(t *testing.T) {
	validOrFatal(t, rec(nil))
	s, err := OpenStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteRecord(rec(nil), now); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadRecord("wdtt", strings.Repeat("a", 64), recordIDOf(rec(nil)), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "wdtt" || got.Result != ResultPASS || got.MeasuredAt != rec(nil).MeasuredAt {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestCorruptedJSONReported(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	if _, err := s.WriteRecord(rec(nil), now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.root, "wdtt", strings.Repeat("a", 64), recordIDOf(rec(nil))+".json")
	if err := os.WriteFile(path, []byte("{corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadRecord("wdtt", strings.Repeat("a", 64), recordIDOf(rec(nil)), now); err == nil {
		t.Fatal("corrupted record must be a typed error")
	}
	// List must REPORT the problem, not skip it silently.
	entries, problems, err := s.ListRecords("wdtt", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(problems) != 1 {
		t.Fatalf("corrupt entry must surface: %d entries, %d problems", len(entries), len(problems))
	}
}

func TestUnknownRecordVersionRefused(t *testing.T) {
	r := rec(func(x *Record) { x.RecordVersion = 2 })
	f := ValidateRecord(r, now)
	if len(f) == 0 || f[0].Code != "RECORD_VERSION" {
		t.Fatalf("version 2 must be flagged: %+v", f)
	}
	if !strings.Contains(ErrUnknownRecordVersion.Error(), "unknown acceptance record version") {
		t.Fatal("sentinel text changed")
	}
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	if _, err := s.WriteRecord(r, now); err == nil {
		t.Fatal("store must refuse unknown record versions")
	}
}

func TestRepeatedWriteRefused(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	if _, err := s.WriteRecord(rec(nil), now); err != nil {
		t.Fatal(err)
	}
	// Same ID, even with modified content — evidence is append-only.
	r2 := rec(func(x *Record) { x.Result = ResultFAIL })
	if _, err := s.WriteRecord(r2, now); err == nil {
		t.Fatal("overwriting a record must be refused (tamper protection)")
	}
}

func TestConcurrentWrites(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	var wg sync.WaitGroup
	errs := make([]error, 12)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := rec(func(x *Record) { x.ProfileLabel = "synthetic-load-" + string(rune('a'+i)) })
			_, errs[i] = s.WriteRecord(r, now)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent write %d failed: %v", i, err)
		}
	}
	entries, problems, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 12 || len(problems) != 0 {
		t.Fatalf("expected 12 clean entries, got %d/%d", len(entries), len(problems))
	}
}

func TestValidationRejections(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Record)
		code string
	}{
		{"bad sha", func(x *Record) { x.ArtifactSHA256 = strings.ToUpper(strings.Repeat("a", 63)) }, "SHA256"},
		{"foreign arch", func(x *Record) { x.Arch = "x86_64" }, "ARCH"},
		{"unknown source", func(x *Record) { x.ResultSource = "SIMULATOR" }, "RESULT_SOURCE"},
		{"env id with space", func(x *Record) { x.EnvironmentID = "has space" }, "ENVIRONMENT_ID"},
		{"bad result", func(x *Record) { x.Result = "OK" }, "RESULT"},
		{"zero time", func(x *Record) { x.MeasuredAt = time.Time{} }, "MEASURED_AT"},
		{"future time", func(x *Record) { x.MeasuredAt = now.Add(time.Hour) }, "MEASURED_AT"},
		{"unknown test", func(x *Record) { x.TestType = "VIBES" }, "TEST_TYPE"},
		{"traversal config", func(x *Record) { x.ConfigRef = "../owner-config.json" }, "CONFIG_REF"},
		{"bad metric name", func(x *Record) { x.Metrics = map[string]float64{"Peak RSS": 1} }, "METRICS"},
		{"traversal artifact", func(x *Record) { x.Artifacts = []string{"a/../../x"} }, "ARTIFACTS"},
	}
	for _, c := range cases {
		f := ValidateRecord(rec(c.mut), now)
		found := false
		for _, x := range f {
			if x.Code == c.code {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: expected %s finding, got %+v", c.name, c.code, f)
		}
	}
}

func TestSecretPatternsRejected(t *testing.T) {
	for _, note := range []string{"api_token=abcdef", "BEGIN PRIVATE KEY", "password: hunter2", "AKIAIOSFODNN7EXAMPLE"} {
		r := rec(func(x *Record) { x.Notes = note })
		f := ValidateRecord(r, now)
		blocked := false
		for _, x := range f {
			if x.Code == "SECRET_PATTERN" {
				blocked = true
			}
		}
		if !blocked {
			t.Fatalf("secret-looking note %q must be rejected, got %+v", note, f)
		}
	}
}

func TestCompatibilityExactMatchOnly(t *testing.T) {
	r := rec(nil)
	if err := CheckCompatibility(r, Want{ProviderID: "wdtt", ArtifactSHA256: strings.Repeat("a", 64), Arch: "arm64", ProfileLabel: "synthetic-18x1-short"}); err != nil {
		t.Fatalf("exact match must pass: %v", err)
	}
	for _, want := range []Want{
		{ProviderID: "openflux"},
		{ArtifactSHA256: strings.Repeat("b", 64)},
		{Arch: "mipsle"},
		{ABI: "glibc-entware"},
		{ProfileLabel: "other-profile"},
		{ResultSource: SourcePhysical},
	} {
		if err := CheckCompatibility(r, want); err == nil {
			t.Fatalf("mismatch %+v must be an error (no evidence inheritance)", want)
		}
	}
}

func TestFreshnessAndEffectiveEvidence(t *testing.T) {
	// Fresh PASS: ceiling applies.
	lvl, ff := EffectiveEvidence(rec(nil), now, 0)
	if lvl != contract.EvidenceBuildConfirmed || len(ff) != 0 {
		t.Fatalf("LOCAL_FAKE fresh PASS must cap at BUILD_CONFIRMED, got %s %+v", lvl, ff)
	}
	// Stale: UNKNOWN.
	stale := rec(func(x *Record) { x.MeasuredAt = now.Add(-91 * 24 * time.Hour) })
	if lvl, _ := EffectiveEvidence(stale, now, 0); lvl != contract.EvidenceUnknown {
		t.Fatalf("stale record must be UNKNOWN, got %s", lvl)
	}
	if ok, f := CheckFreshness(stale, now, 0); ok || f[0].Code != "MEASURED_STALE" {
		t.Fatalf("stale must be reported: %v", f)
	}
	// FAIL verdict: UNKNOWN regardless of source.
	failed := rec(func(x *Record) { x.Result = ResultFAIL })
	if lvl, _ := EffectiveEvidence(failed, now, 0); lvl != contract.EvidenceUnknown {
		t.Fatalf("failed run must be UNKNOWN, got %s", lvl)
	}
}

// The governing rule (task 14 C3): synthetic results never become physical
// E2E proof — and even PHYSICAL evidence requires an E2E-type test.
func TestSyntheticNeverPromotesToE2E(t *testing.T) {
	for _, src := range []string{SourceLocalFake, SourceQemuUser, SourceEmulator} {
		r := rec(func(x *Record) {
			x.ResultSource = src
			x.EnvironmentID = "guest-01"
		})
		if f := ValidateRecord(r, now); len(f) != 0 {
			t.Fatalf("%s record must be valid: %+v", src, f)
		}
		lvl, _ := EffectiveEvidence(r, now, 0)
		if lvl == contract.EvidenceE2EVerified {
			t.Fatalf("%s PASS must never yield E2E_VERIFIED", src)
		}
		if lvl != contract.EvidenceBuildConfirmed {
			t.Fatalf("%s fresh PASS caps at BUILD_CONFIRMED, got %s", src, lvl)
		}
	}
	// PHYSICAL: ceiling only for E2E test types.
	phys := rec(func(x *Record) { x.ResultSource = SourcePhysical; x.EnvironmentID = "NC-LAB-01" })
	if lvl, _ := EffectiveEvidence(phys, now, 0); lvl != contract.EvidenceE2EVerified {
		t.Fatalf("physical TCP_E2E PASS may reach the E2E ceiling, got %s", lvl)
	}
	physOther := rec(func(x *Record) {
		x.ResultSource = SourcePhysical
		x.EnvironmentID = "NC-LAB-01"
		x.TestType = TestLatency
	})
	if lvl, _ := EffectiveEvidence(physOther, now, 0); lvl != contract.EvidenceBuildConfirmed {
		t.Fatalf("physical non-E2E test caps at BUILD_CONFIRMED, got %s", lvl)
	}
	// The store itself never writes evidence anywhere: nothing in this
	// package can mutate a catalog or manifest.
}

func TestOversizeAndTrailingJSON(t *testing.T) {
	huge := []byte(`{"record_version":1,"pad":"` + strings.Repeat("x", MaxRecordBytes) + `"}`)
	if _, err := decodeRecord(huge); err == nil {
		t.Fatal("oversize record must be refused")
	}
	if _, err := decodeRecord([]byte(`{"record_version":1} {}`)); err == nil {
		t.Fatal("trailing JSON must be refused")
	}
	if _, err := decodeRecord([]byte(`{"record_version":1,"surprise":true}`)); err == nil {
		t.Fatal("unknown fields must be refused")
	}
}

func TestPathTraversalImpossible(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	if _, err := s.recordPath("../evil", strings.Repeat("a", 64), "tcp_e2e-x-y"); err == nil {
		t.Fatal("provider traversal must be rejected")
	}
	if _, err := s.recordPath("wdtt", strings.Repeat("a", 64), "../../evil"); err == nil {
		t.Fatal("record id traversal must be rejected")
	}
}

func TestReadRecordValidatesStoredContent(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	if _, err := s.WriteRecord(rec(nil), now); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.root, "wdtt", strings.Repeat("a", 64), recordIDOf(rec(nil))+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Tamper: inject an evidence-level field into the stored JSON (the
	// realistic attack: someone tries to append E2E proof to a synthetic
	// record). Unknown fields break strict decode on the next read.
	tampered := strings.Replace(string(b),
		`"result": "PASS"`,
		`"result": "PASS","evidence_level":"E2E_VERIFIED"`, 1)
	if tampered == string(b) {
		t.Fatal("tamper replacement did not apply")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadRecord("wdtt", strings.Repeat("a", 64), recordIDOf(rec(nil)), now); err == nil {
		t.Fatal("tampered record must fail re-validation")
	}
}

func TestStorePermissionsAndMissingRoot(t *testing.T) {
	if _, err := OpenStore(""); err == nil {
		t.Fatal("empty root must be rejected")
	}
	s, err := OpenStore(filepath.Join(t.TempDir(), "nested", "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteRecord(rec(nil), now); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		return // POSIX permission bits are not enforced on Windows
	}
	info, err := os.Stat(filepath.Join(s.root, "wdtt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("record directory must be 0700, got %v", info.Mode().Perm())
	}
}

func TestListSelectorSHAFilter(t *testing.T) {
	s, _ := OpenStore(filepath.Join(t.TempDir(), "store"))
	if _, err := s.WriteRecord(rec(nil), now); err != nil {
		t.Fatal(err)
	}
	other := rec(func(x *Record) { x.ArtifactSHA256 = strings.Repeat("b", 64) })
	if _, err := s.WriteRecord(other, now); err != nil {
		t.Fatal(err)
	}
	entries, _, err := s.ListRecords("wdtt", strings.Repeat("b", 64), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ArtifactSHA != strings.Repeat("b", 64) {
		t.Fatalf("sha selector must filter: %+v", entries)
	}
}
