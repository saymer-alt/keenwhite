// Package contract is the single source of truth for checks shared by the
// catalog (`keenwhite.dev/v0alpha1`), the provider contract
// (`keenwhite.provider/v1`) and the KAS MT-001/MT-002 research lineage.
// It exists so the catalog and the Provider API can never disagree about
// the same semantics (task #13, issue #4). Everything is offline.
package contract

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Severity of a Finding. ERROR blocks Prepare/Start; WARNING requires
// attention but does not always block; UNSUPPORTED means the feature is not
// supported by this provider; UNKNOWN means there is not enough verified
// data to judge.
type Severity string

const (
	SeverityError       Severity = "ERROR"
	SeverityWarning     Severity = "WARNING"
	SeverityUnsupported Severity = "UNSUPPORTED"
	SeverityUnknown     Severity = "UNKNOWN"
)

// Finding is one validation result in the unified vocabulary.
type Finding struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Field    string   `json:"field"`
	Message  string   `json:"message"`
}

// Evidence levels shared by every contract generation. Values are identical
// across catalog, Provider v1 and MT-001/MT-002.
const (
	EvidenceUnknown         = "UNKNOWN"
	EvidenceUnsupported     = "UNSUPPORTED"
	EvidenceSourceConfirmed = "SOURCE_CONFIRMED"
	EvidenceBuildConfirmed  = "BUILD_CONFIRMED"
	EvidenceE2EVerified     = "E2E_VERIFIED"
)

var validEvidence = map[string]bool{
	EvidenceUnknown: true, EvidenceUnsupported: true,
	EvidenceSourceConfirmed: true, EvidenceBuildConfirmed: true,
	EvidenceE2EVerified: true,
}

// DefaultMeasurementTTL is how long an E2E measurement stays credible.
const DefaultMeasurementTTL = 90 * 24 * time.Hour

// MaxClockSkew tolerates small clock differences for measurement dates.
const MaxClockSkew = 5 * time.Minute

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidEvidence reports whether the level is one of the five contract levels.
func ValidEvidence(level string) bool { return validEvidence[level] }

// CheckEvidence is THE shared evidence rule (issue #4): an E2E_VERIFIED
// claim requires an evidence URL and a PAST measured date within ttl; a
// future-dated measurement is corrupt; staleness downgrades E2E to UNKNOWN.
// SOURCE_CONFIRMED/BUILD_CONFIRMED are never upgraded, and their freshness
// is not time-bound (finding UNKNOWN, advisory only). Non-E2E levels pass
// through unchanged.
//
//	level        evidence level as written in the document
//	url          evidence URL ("" = absent)
//	measured     original measurement time (nil = absent)
//	ttl          claim lifetime (<=0 -> DefaultMeasurementTTL)
//	now          injected clock reading
//
// Returns the EFFECTIVE level after fail-closed downgrades.
func CheckEvidence(level, url string, measured *time.Time, ttl time.Duration, now time.Time) (string, []Finding) {
	if !ValidEvidence(level) {
		return EvidenceUnknown, []Finding{{SeverityError, "EVIDENCE_LEVEL", "evidence",
			fmt.Sprintf("unknown evidence level %q", level)}}
	}
	if level != EvidenceE2EVerified {
		return level, nil
	}
	var f []Finding
	if url == "" {
		f = append(f, Finding{SeverityError, "E2E_NO_URL", "evidence.evidence_url",
			"E2E_VERIFIED requires an evidence URL"})
		return EvidenceUnknown, f
	}
	if measured == nil {
		f = append(f, Finding{SeverityError, "E2E_NO_MEASUREMENT", "evidence.measured_at",
			"E2E_VERIFIED requires a measurement date"})
		return EvidenceUnknown, f
	}
	if measured.After(now.Add(MaxClockSkew)) {
		f = append(f, Finding{SeverityError, "E2E_FUTURE_DATE", "evidence.measured_at",
			"measurement is dated in the future; corrupt evidence is rejected"})
		return EvidenceUnknown, f
	}
	if ttl <= 0 {
		ttl = DefaultMeasurementTTL
	}
	if now.Sub(*measured) > ttl {
		f = append(f, Finding{SeverityError, "E2E_STALE", "evidence.measured_at",
			fmt.Sprintf("measurement is older than its TTL (%s); stale results cannot claim E2E_VERIFIED", ttl)})
		return EvidenceUnknown, f
	}
	return level, f
}

// ProviderRef is the minimal identity used for cross-provider checks.
type ProviderRef struct {
	ID   string // provider id (catalog/manifest)
	Bind string // loopback SOCKS bind "127.0.0.1:port"
}

var providerIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// CheckProviderSet validates a SET of providers against each other:
// duplicate IDs and SOCKS bind conflicts across the whole set. Per-provider
// syntax rules stay with their own packages; only cross-provider semantics
// live here.
func CheckProviderSet(refs []ProviderRef) []Finding {
	var f []Finding
	ids := map[string]string{}
	binds := map[string]string{}
	for _, r := range refs {
		if !providerIDRE.MatchString(r.ID) {
			f = append(f, Finding{SeverityError, "PROVIDER_ID", "provider",
				fmt.Sprintf("invalid provider id %q", r.ID)})
			continue
		}
		if prev, ok := ids[r.ID]; ok {
			f = append(f, Finding{SeverityError, "DUPLICATE_PROVIDER", "provider",
				fmt.Sprintf("provider id %q already used by %s", r.ID, prev)})
		}
		ids[r.ID] = r.ID
		if prev, ok := binds[r.Bind]; ok && prev != r.ID {
			f = append(f, Finding{SeverityError, "BIND_CONFLICT", "socks.bind",
				fmt.Sprintf("bind %s shared by %s and %s", r.Bind, prev, r.ID)})
		}
		if r.Bind != "" {
			binds[r.Bind] = r.ID
		}
	}
	return f
}

// ErrUnknownSchemaVersion is returned when a document declares a contract
// version this build does not know. Unknown versions are NEVER accepted
// silently (task #13 stage C, rule 2).
var ErrUnknownSchemaVersion = errors.New("unknown schema version: refusing silent interpretation")

// MTSchemaVersion is the only MT-001 provider schema version mapped here.
const MTSchemaVersion = 1

// MappedProviderV1 is the Provider-v1-shaped projection of an MT-001
// provider manifest. Fields absent from MT-001 stay zero and are reported
// in Findings — nothing is invented, nothing is dropped silently.
type MappedProviderV1 struct {
	ManifestVersion int
	APIVersion      string
	Provider        string
	ArtifactPath    string
	ArtifactSHA256  string
	Arch            string
	ProfilePath     string // from configuration_reference
	SocksBind       string
	TCP             string
	UDP             string
	MinMemMB        int // 0 = no floor (MT null admission_min_mb)
	KASCombinedRef  string
	HardStopSeconds int // 0 = not set by MT-001 (WDTT-specific)
}

// ParseMTProviderManifest validates an MT-001 provider manifest (schema
// _version == 1) and maps it onto the Provider v1 shape.
//
// Fidelity rules (task #13 stage C): unknown schema versions are refused;
// every unmapped MT field is reported as a WARNING finding (no silent
// field loss); evidence levels are carried verbatim — UNKNOWN is never
// turned into PASS and SOURCE_CONFIRMED is never raised to E2E_VERIFIED;
// the KAS ≥512 MB combined policy travels only as the documentation string
// it is, never as a global KeenWhite gate.
func ParseMTProviderManifest(doc []byte, now time.Time) (MappedProviderV1, []Finding, error) {
	var out MappedProviderV1
	mt, err := parseMTDoc(doc)
	if err != nil {
		return out, nil, err
	}
	if mt.SchemaVersion != MTSchemaVersion {
		return out, nil, fmt.Errorf("%w: schema_version %d, want %d",
			ErrUnknownSchemaVersion, mt.SchemaVersion, MTSchemaVersion)
	}
	out.ManifestVersion = 1
	out.APIVersion = "keenwhite.provider/v1"
	out.Provider = mt.ProviderID
	out.ArtifactPath = mt.ArtifactProvenance.ArtifactPath
	out.ArtifactSHA256 = mt.ArtifactProvenance.SHA256
	out.Arch = mt.Architecture
	out.ProfilePath = mt.ConfigurationReference
	out.SocksBind = mt.InboundBind
	out.TCP = capLevel(mt.Capabilities, "tcp")
	out.UDP = capLevel(mt.Capabilities, "udp")
	if mt.ResourceRequirements != nil {
		out.MinMemMB = mt.ResourceRequirements.AdmissionMinMB
		out.KASCombinedRef = mt.ResourceRequirements.AdmissionSource
	}

	var f []Finding
	add := func(sev Severity, code, field, msg string) {
		f = append(f, Finding{sev, code, field, msg})
	}
	if mt.AdapterVersion != "" {
		add(SeverityWarning, "MT_ADAPTER_VERSION", "adapter_version",
			"MT adapter_version has no Provider-v1 field; carried in findings only")
	}
	if mt.ServerWire != "" {
		add(SeverityWarning, "MT_SERVER_WIRE", "server_wire",
			"MT server_wire lives in the KeenWhite catalog (wire), not in the v1 manifest")
	}
	if mt.ServerClass != "" {
		add(SeverityWarning, "MT_SERVER_CLASS", "server_class",
			"MT server_class lives in the KeenWhite catalog (server_class), not in the v1 manifest")
	}
	if mt.ArtifactProvenance.SourceCommit != "" {
		add(SeverityWarning, "MT_SOURCE_COMMIT", "artifact_provenance.source_commit",
			"MT source_commit has no Provider-v1 field; keep it in the migration ledger")
	}
	// Evidence mapping: MT capabilities carry static code/build levels only.
	// The runtime evidence enum is the same five values; nothing is upgraded.
	for _, axis := range []struct {
		name, level string
	}{
		{"tcp", capLevel(mt.Capabilities, "tcp")},
		{"udp", capLevel(mt.Capabilities, "udp")},
	} {
		if !ValidEvidence(axis.level) {
			add(SeverityError, "MT_CAPABILITY_LEVEL", "capabilities."+axis.name,
				fmt.Sprintf("unknown capability level %q", axis.level))
		} else if axis.level == EvidenceE2EVerified {
			add(SeverityWarning, "MT_E2E_UNBACKED", "capabilities."+axis.name,
				"MT manifest cannot carry measured_at/URL; E2E_VERIFIED is downgraded to UNKNOWN in Provider v1")
			if axis.name == "tcp" {
				out.TCP = EvidenceUnknown
			} else {
				out.UDP = EvidenceUnknown
			}
		}
	}
	if mt.ResourceRequirements != nil && mt.ResourceRequirements.Measured {
		add(SeverityUnknown, "MT_MEASURED_FLAG", "resource_requirements.measured",
			"measured=true refers to KAS lab data; KeenWhite re-measures at Prepare/Start")
	}
	eff, ef := CheckEvidence(out.TCP, "", nil, 0, now)
	out.TCP = eff
	f = append(f, ef...)
	eff, ef = CheckEvidence(out.UDP, "", nil, 0, now)
	out.UDP = eff
	f = append(f, ef...)
	return out, f, nil
}
