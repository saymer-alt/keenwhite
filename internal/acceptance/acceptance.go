// Package acceptance is KeenWhite's offline evidence store for verification
// runs (task 14 stage C). It records what was tested, where, on which exact
// artifact, when — and with which result source. It is strictly offline:
// no network, no processes, no router access.
//
// The governing rule (task 14 C3): a synthetic result never becomes physical
// E2E proof. EvidenceLevelOf caps every record by its result source; only a
// PHYSICAL TCP_E2E/UDP_E2E run can ever back an E2E-level claim, and even
// that stays a ceiling the owner must confirm — nothing here ASSIGNS
// evidence levels to providers.
package acceptance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/saymer-alt/keenwhite/internal/contract"
)

// RecordVersion is the only acceptance record version this build accepts.
const RecordVersion = 1

// APIVersion identifies the record schema for cross-tool consumption.
const APIVersion = "keenwhite.acceptance/v1"

// MaxRecordBytes bounds one record on disk.
const MaxRecordBytes = 64 * 1024

// ResultSource separates what kind of environment produced a result.
// Synthetic sources (LOCAL_FAKE, QEMU_USER) and guest-only sources
// (KEENETIC_EMULATOR) can never back a physical transport claim.
const (
	SourceLocalFake = "LOCAL_FAKE"
	SourceQemuUser  = "QEMU_USER"
	SourceEmulator  = "KEENETIC_EMULATOR"
	SourcePhysical  = "PHYSICAL"
)

var resultSources = map[string]bool{
	SourceLocalFake: true, SourceQemuUser: true, SourceEmulator: true, SourcePhysical: true,
}

// Test types (aligned with the MT-002 research lineage where meaningful).
const (
	TestTCPE2E      = "TCP_E2E"
	TestUDPE2E      = "UDP_E2E"
	TestRAMBaseline = "RAM_BASELINE"
	TestRAMSoak     = "RAM_SOAK"
	TestFailClosed  = "FAIL_CLOSED"
	TestReboot      = "REBOOT"
	TestLatency     = "LATENCY"
	TestThroughput  = "THROUGHPUT"
	TestOther       = "OTHER"
)

var testTypes = map[string]bool{
	TestTCPE2E: true, TestUDPE2E: true, TestRAMBaseline: true, TestRAMSoak: true,
	TestFailClosed: true, TestReboot: true, TestLatency: true, TestThroughput: true, TestOther: true,
}

// Results are the five stored verdicts (task 14 C2). They are RUN verdicts,
// not provider evidence levels; a PASS never leaks out as E2E_VERIFIED
// through EvidenceLevelOf unless the source ceiling allows it.
const (
	ResultPASS        = "PASS"
	ResultFAIL        = "FAIL"
	ResultNotTested   = "NOT_TESTED"
	ResultUnsupported = "UNSUPPORTED"
	ResultUnknown     = "UNKNOWN"
)

var results = map[string]bool{
	ResultPASS: true, ResultFAIL: true, ResultNotTested: true,
	ResultUnsupported: true, ResultUnknown: true,
}

var (
	idPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)
	shaPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	archPattern   = regexp.MustCompile(`^(arm64|mipsle)$`)
	abiPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)
	envIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,63}$`)
	labelPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@/-]{0,63}$`)
	refPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	metricPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
	recordPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{3,63}$`)
)

// Record is one verification run. Only synthetic identifiers and numeric
// metrics belong here — no credentials, private URIs or raw logs; free-text
// fields are denied-list checked for secret-looking content.
type Record struct {
	RecordVersion   int                `json:"record_version"`
	ProviderID      string             `json:"provider_id"`
	ArtifactSHA256  string             `json:"artifact_sha256"`
	Arch            string             `json:"arch"`
	ABI             string             `json:"abi,omitempty"`
	ResultSource    string             `json:"result_source"`
	EnvironmentID   string             `json:"environment_id"`
	EnvironmentNote string             `json:"environment_note,omitempty"` // e.g. guest image id/hash (synthetic)
	OSVersion       string             `json:"os_version,omitempty"`       // e.g. "5.1.8" or "host-linux"
	ProfileLabel    string             `json:"profile_label"`
	ConfigRef       string             `json:"config_ref,omitempty"` // secret-free config identifier
	ContractVersion string             `json:"contract_version"`
	TestType        string             `json:"test_type"`
	MeasuredAt      time.Time          `json:"measured_at"`
	DurationSec     int                `json:"duration_sec,omitempty"`
	Result          string             `json:"result"`
	CleanupOK       *bool              `json:"cleanup_ok,omitempty"`
	Metrics         map[string]float64 `json:"metrics,omitempty"`
	Artifacts       []string           `json:"artifacts,omitempty"`
	Notes           string             `json:"notes,omitempty"`
}

// ErrUnknownRecordVersion refuses records from a schema this build does not
// know — silent interpretation is never allowed.
var ErrUnknownRecordVersion = errors.New("unknown acceptance record version: refusing silent interpretation")

// ValidateRecord checks one record against the v1 rules and returns typed
// findings; empty slice = valid. now is injected for determinism.
func ValidateRecord(r Record, now time.Time) []contract.Finding {
	var f []contract.Finding
	add := func(sev contract.Severity, code, field, msg string) {
		f = append(f, contract.Finding{Severity: sev, Code: code, Field: field, Message: msg})
	}
	if r.RecordVersion != RecordVersion {
		add(contract.SeverityError, "RECORD_VERSION", "record_version",
			fmt.Sprintf("must be %d", RecordVersion))
	}
	if !idPattern.MatchString(r.ProviderID) {
		add(contract.SeverityError, "PROVIDER_ID", "provider_id", "invalid provider id")
	}
	if !shaPattern.MatchString(r.ArtifactSHA256) {
		add(contract.SeverityError, "SHA256", "artifact_sha256", "must be exactly 64 lowercase hex characters")
	}
	if !archPattern.MatchString(r.Arch) {
		add(contract.SeverityError, "ARCH", "arch", "must be arm64 or mipsle")
	}
	if r.ABI != "" && !abiPattern.MatchString(r.ABI) {
		add(contract.SeverityError, "ABI", "abi", "invalid abi")
	}
	if !resultSources[r.ResultSource] {
		add(contract.SeverityError, "RESULT_SOURCE", "result_source",
			"must be LOCAL_FAKE, QEMU_USER, KEENETIC_EMULATOR or PHYSICAL")
	}
	if !envIDPattern.MatchString(r.EnvironmentID) {
		add(contract.SeverityError, "ENVIRONMENT_ID", "environment_id", "synthetic stand/guest identifier required (no serials, no private addresses)")
	}
	if r.EnvironmentNote != "" && len(r.EnvironmentNote) > 256 {
		add(contract.SeverityError, "ENVIRONMENT_NOTE", "environment_note", "must be at most 256 characters")
	}
	if r.OSVersion != "" && (len(r.OSVersion) > 32 || !envIDPattern.MatchString(r.OSVersion)) {
		add(contract.SeverityError, "OS_VERSION", "os_version", "must be a short synthetic version label")
	}
	if !labelPattern.MatchString(r.ProfileLabel) {
		add(contract.SeverityError, "PROFILE_LABEL", "profile_label", "synthetic workload/profile label required")
	}
	if r.ConfigRef != "" && (!refPattern.MatchString(r.ConfigRef) || hasParentSeg(r.ConfigRef)) {
		add(contract.SeverityError, "CONFIG_REF", "config_ref", "must be a secret-free reference path/label")
	}
	if len(r.ContractVersion) < 4 || len(r.ContractVersion) > 64 {
		add(contract.SeverityError, "CONTRACT_VERSION", "contract_version", "must name the tested contract (4..64 chars)")
	}
	if !testTypes[r.TestType] {
		add(contract.SeverityError, "TEST_TYPE", "test_type", "unknown test type")
	}
	if r.MeasuredAt.IsZero() {
		add(contract.SeverityError, "MEASURED_AT", "measured_at", "measurement time required")
	} else if r.MeasuredAt.Sub(now) > contract.MaxClockSkew {
		add(contract.SeverityError, "MEASURED_AT", "measured_at", "measurement time is in the future; corrupt record")
	}
	if r.DurationSec < 0 || r.DurationSec > 2592000 {
		add(contract.SeverityError, "DURATION", "duration_sec", "must be 0..2592000 seconds")
	}
	if !results[r.Result] {
		add(contract.SeverityError, "RESULT", "result", "must be PASS, FAIL, NOT_TESTED, UNSUPPORTED or UNKNOWN")
	}
	if len(r.Metrics) > 32 {
		add(contract.SeverityError, "METRICS", "metrics", "at most 32 metrics")
	}
	for name := range r.Metrics {
		if !metricPattern.MatchString(name) {
			add(contract.SeverityError, "METRICS", "metrics."+name, "invalid metric name")
			break
		}
	}
	if len(r.Artifacts) > 8 {
		add(contract.SeverityError, "ARTIFACTS", "artifacts", "at most 8 artifact references")
	}
	for _, a := range r.Artifacts {
		if !refPattern.MatchString(a) || hasParentSeg(a) {
			add(contract.SeverityError, "ARTIFACTS", "artifacts", fmt.Sprintf("invalid artifact reference %q", a))
			break
		}
	}
	if len(r.Notes) > 512 {
		add(contract.SeverityError, "NOTES", "notes", "must be at most 512 characters")
	}
	for _, hit := range secretScan(r.EnvironmentNote, r.ConfigRef, r.Notes) {
		add(contract.SeverityError, "SECRET_PATTERN", hit.field, "record fields must never contain credentials or secret-looking text: "+hit.what)
	}
	return f
}

var secretDenylist = []string{
	"password", "passwd", "secret", "token", "credential",
	"private key", "authorization", "bearer ", "akia", "ghp_", "xoxb-",
}

type secretHit struct{ field, what string }

// secretScan is a deliberately simple denylist over free-text fields: the
// store accepts only synthetic identifiers, so any credential-looking
// substring is a mistake and must be rejected at write time.
func secretScan(environmentNote, configRef, notes string) []secretHit {
	var hits []secretHit
	for _, fl := range []struct{ name, value string }{
		{"environment_note", environmentNote}, {"config_ref", configRef}, {"notes", notes},
	} {
		lower := strings.ToLower(fl.value)
		for _, bad := range secretDenylist {
			if strings.Contains(lower, bad) {
				hits = append(hits, secretHit{fl.name, strconv.Quote(bad)})
				break
			}
		}
	}
	return hits
}

func hasParentSeg(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// EvidenceLevelOf returns the EVIDENCE CEILING a record can back (task 14
// C3): the mapping from result source to the strongest provider-evidence
// level the run could possibly support. It is a ceiling, never an
// assignment: the catalog/manifest keep their own claims, and nothing in
// this package writes E2E_VERIFIED anywhere.
//
//	LOCAL_FAKE        → BUILD_CONFIRMED  (tests code behaviour, not transport)
//	QEMU_USER         → BUILD_CONFIRMED  (binary verified under CPU emulation)
//	KEENETIC_EMULATOR → BUILD_CONFIRMED  (guest-level result; no real carrier)
//	PHYSICAL          → E2E_VERIFIED     (only a real device run can ever back E2E)
func EvidenceLevelOf(source string) string {
	switch source {
	case SourcePhysical:
		return contract.EvidenceE2EVerified
	case SourceLocalFake, SourceQemuUser, SourceEmulator:
		return contract.EvidenceBuildConfirmed
	default:
		return contract.EvidenceUnknown
	}
}

// EffectiveEvidence combines the ceiling with the run verdict and freshness:
// only a fresh PASS on an E2E-capable test type yields the source ceiling;
// everything else is bounded by UNKNOWN/BUILD_CONFIRMED as applicable.
// tcp/udp style tests map to their axis; other test types are informational
// and never produce E2E-level evidence.
func EffectiveEvidence(r Record, now time.Time, ttl time.Duration) (string, []contract.Finding) {
	ceiling := EvidenceLevelOf(r.ResultSource)
	fresh, ff := CheckFreshness(r, now, ttl)
	if !fresh {
		return contract.EvidenceUnknown, ff
	}
	if r.Result != ResultPASS {
		ff = append(ff, contract.Finding{
			Severity: contract.SeverityUnknown, Code: "RUN_NOT_PASS", Field: "result",
			Message: fmt.Sprintf("run verdict %s cannot back an evidence claim", r.Result),
		})
		return contract.EvidenceUnknown, ff
	}
	e2eCapable := r.TestType == TestTCPE2E || r.TestType == TestUDPE2E
	if !e2eCapable && ceiling == contract.EvidenceE2EVerified {
		return contract.EvidenceBuildConfirmed, ff
	}
	return ceiling, ff
}

// CheckFreshness applies the shared evidence clock rule: future dates are
// corrupt; entries older than ttl are stale. A record without a usable
// measurement time is never fresh.
func CheckFreshness(r Record, now time.Time, ttl time.Duration) (bool, []contract.Finding) {
	if r.MeasuredAt.IsZero() {
		return false, []contract.Finding{{Severity: contract.SeverityError, Code: "MEASURED_AT",
			Field: "measured_at", Message: "no measurement time"}}
	}
	if r.MeasuredAt.After(now.Add(contract.MaxClockSkew)) {
		return false, []contract.Finding{{Severity: contract.SeverityError, Code: "MEASURED_FUTURE",
			Field: "measured_at", Message: "measurement is dated in the future"}}
	}
	if ttl <= 0 {
		ttl = contract.DefaultMeasurementTTL
	}
	if now.Sub(r.MeasuredAt) > ttl {
		return false, []contract.Finding{{Severity: contract.SeverityError, Code: "MEASURED_STALE",
			Field: "measured_at", Message: fmt.Sprintf("measurement is older than its TTL (%s)", ttl)}}
	}
	return true, nil
}

// Want is the exact compatibility selector: every set field must match the
// record. Evidence is never inherited across binaries, architectures,
// profiles or environments.
type Want struct {
	ProviderID     string
	ArtifactSHA256 string
	Arch           string
	ABI            string // optional; checked only when non-empty
	ProfileLabel   string
	ResultSource   string // optional; checked only when non-empty
}

// CheckCompatibility reports the first mismatch between a record and the
// selector as a typed error. Nothing is inferred: an absent field never
// matches.
func CheckCompatibility(r Record, want Want) error {
	switch {
	case want.ProviderID != "" && r.ProviderID != want.ProviderID:
		return fmt.Errorf("provider mismatch: record %q != requested %q", r.ProviderID, want.ProviderID)
	case want.ArtifactSHA256 != "" && !strings.EqualFold(r.ArtifactSHA256, want.ArtifactSHA256):
		return fmt.Errorf("artifact mismatch: evidence is valid only for the exact binary SHA256")
	case want.Arch != "" && r.Arch != want.Arch:
		return fmt.Errorf("arch mismatch: record %q != requested %q", r.Arch, want.Arch)
	case want.ABI != "" && r.ABI != want.ABI:
		return fmt.Errorf("abi mismatch: record %q != requested %q", r.ABI, want.ABI)
	case want.ProfileLabel != "" && r.ProfileLabel != want.ProfileLabel:
		return fmt.Errorf("profile mismatch: record %q != requested %q", r.ProfileLabel, want.ProfileLabel)
	case want.ResultSource != "" && r.ResultSource != want.ResultSource:
		return fmt.Errorf("result source mismatch: record %q != requested %q", r.ResultSource, want.ResultSource)
	}
	return nil
}

// decodeRecord strictly decodes record bytes: unknown fields, trailing JSON
// and oversize documents are refused (corrupt-record protection).
func decodeRecord(b []byte) (Record, error) {
	var r Record
	if len(b) > MaxRecordBytes {
		return r, fmt.Errorf("acceptance record exceeds %d bytes", MaxRecordBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Record{}, fmt.Errorf("decode acceptance record: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Record{}, errors.New("unexpected trailing JSON in acceptance record")
		}
		return Record{}, fmt.Errorf("invalid trailing acceptance input: %w", err)
	}
	return r, nil
}
