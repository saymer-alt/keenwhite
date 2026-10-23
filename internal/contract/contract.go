// Package contract is the single source of truth for checks shared by the
// catalog (`keenwhite.dev/v0alpha1`), the provider contract
// (`keenwhite.provider/v1`) and the KAS MT-001/MT-002 research lineage.
// It exists so the catalog and the Provider API can never disagree about
// the same semantics (task #13, issue #4). Everything is offline.
package contract

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
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

// MappedProviderV1 is the Provider-v1-shaped projection of an MT-001
// provider manifest. Fields without a Provider v1 equivalent stay zero AND
// are carried verbatim in the MT-specific fields below plus reported as
// WARNING findings — nothing is invented, nothing is dropped silently
// (task 14 A4).
type MappedProviderV1 struct {
	ManifestVersion int
	APIVersion      string
	Provider        string
	ArtifactPath    string
	ArtifactSHA256  string
	// Arch is the canonical KeenWhite architecture (arm64|mipsle) of the
	// ONE architecture selected for execution; "" when ambiguous or
	// unsupported. No arbitrary choice is made among several candidates.
	Arch    string
	ArchABI string // verbatim ABI of the selected entry; no compatibility claim
	// Architectures is EVERY MT-001 architecture entry verbatim —
	// unmapped/unsupported variants are never pruned from diagnostics.
	Architectures []MTArchitecture
	// Capabilities is EVERY MT-001 capability verbatim (static code/build).
	Capabilities    map[string]MTCapability
	ProfilePath     string // from configuration_reference
	SocksBind       string
	TCP             string // effective Provider v1 evidence for socks_tcp
	UDP             string // effective Provider v1 evidence for socks_udp
	MinMemMB        int    // 0 = no floor (MT null admission_min_mb)
	KASCombinedRef  string
	HardStopSeconds int // 0 = not set by MT-001 (WDTT-specific)

	// Verbatim MT-001 metadata with no Provider v1 home (A4: no silent loss).
	AdapterVersion string
	ServerWire     string
	ServerClass    string
	SourceCommit   string
	BuildRef       string
	MeasurementRef string
	ResourceNotes  string
	InboundBindMT  string // MT-001 inbound_bind verbatim (same as SocksBind)
}

// mtCapabilityEvidence is THE documented correspondence rule between MT-001
// static capabilities (code/build) and Provider v1 evidence levels
// (task 14 A3). Static attributes never become runtime evidence: the table
// is monotonic, never promotes above BUILD_CONFIRMED, and can never produce
// E2E_VERIFIED.
//
//	code     build     → evidence         rationale
//	PRESENT  PRESENT   → BUILD_CONFIRMED  in sources and built into this artifact
//	PRESENT  UNKNOWN   → SOURCE_CONFIRMED sources confirm; build state unknown
//	PRESENT  ABSENT    → UNSUPPORTED      sources have it, this artifact does not
//	ABSENT   PRESENT   → (invalid)        schema monotonic invariant violation
//	ABSENT   ABSENT    → UNSUPPORTED      artifact statically lacks it
//	ABSENT   UNKNOWN   → UNKNOWN          sources lack it; build unverifiable
//	UNKNOWN  PRESENT   → (invalid)        schema monotonic invariant violation
//	UNKNOWN  UNKNOWN   → UNKNOWN          insufficient data
//	UNKNOWN  ABSENT    → UNSUPPORTED      artifact lacks it regardless of sources
func mtCapabilityEvidence(cap MTCapability) (string, bool) {
	switch {
	case cap.Build == "PRESENT" && cap.Code == "PRESENT":
		return EvidenceBuildConfirmed, true
	case cap.Build == "UNKNOWN" && cap.Code == "PRESENT":
		return EvidenceSourceConfirmed, true
	case cap.Build == "ABSENT":
		return EvidenceUnsupported, true
	case cap.Build == "UNKNOWN" && cap.Code == "ABSENT":
		return EvidenceUnknown, true
	case cap.Build == "UNKNOWN" && cap.Code == "UNKNOWN":
		return EvidenceUnknown, true
	default:
		// build=PRESENT with code≠PRESENT: invariant violation — no mapping.
		return "", false
	}
}

// mapMTToProviderV1 projects a schema-valid MTDocument onto
// MappedProviderV1. Every mapping difference becomes a typed finding;
// findings are appended to the schema findings already present.
func mapMTToProviderV1(d MTDocument, f []Finding, now time.Time) (MappedProviderV1, []Finding) {
	var out MappedProviderV1
	out.ManifestVersion = 1
	out.APIVersion = "keenwhite.provider/v1"
	out.Provider = d.ProviderID
	out.ArtifactPath = d.ArtifactProvenance.ArtifactPath
	out.ArtifactSHA256 = d.ArtifactProvenance.SHA256
	out.ProfilePath = d.ConfigurationReference
	out.SocksBind = d.InboundBind
	out.InboundBindMT = d.InboundBind
	out.AdapterVersion = d.AdapterVersion
	out.ServerWire = d.ServerWire
	out.ServerClass = d.ServerClass
	out.SourceCommit = d.ArtifactProvenance.SourceCommit
	out.BuildRef = d.ArtifactProvenance.BuildRef
	out.MeasurementRef = derefString(d.ResourceRequirements.MeasurementRef)
	out.ResourceNotes = d.ResourceRequirements.Notes
	out.MinMemMB = derefInt(d.ResourceRequirements.AdmissionMinMB)
	out.KASCombinedRef = derefString(d.ResourceRequirements.AdmissionSource)
	out.Architectures = append([]MTArchitecture(nil), d.Architecture...)
	out.Capabilities = make(map[string]MTCapability, len(d.Capabilities))
	for name, cap := range d.Capabilities {
		out.Capabilities[name] = cap
	}

	add := func(sev Severity, code, field, msg string) {
		f = append(f, Finding{sev, code, field, msg})
	}

	// Architecture selection: map ONLY through MTArchToKeenWhite; never pick
	// arbitrarily among several candidates.
	var selected []MTArchitecture
	for _, a := range d.Architecture {
		if _, ok := MTArchToKeenWhite[a.Arch]; ok {
			selected = append(selected, a)
		} else {
			add(SeverityWarning, "MT_ARCH_UNMAPPED", "architecture[].arch",
				fmt.Sprintf("architecture %s/%s has no KeenWhite target name; kept verbatim, never silently renamed", a.Arch, a.ABI))
		}
	}
	switch {
	case len(selected) == 1:
		out.Arch = MTArchToKeenWhite[selected[0].Arch]
		out.ArchABI = selected[0].ABI
	case len(selected) > 1:
		names := make([]string, 0, len(selected))
		for _, a := range selected {
			names = append(names, a.Arch+"="+MTArchToKeenWhite[a.Arch])
		}
		add(SeverityError, "MT_ARCH_AMBIGUOUS", "architecture",
			fmt.Sprintf("%d architectures map to KeenWhite targets (%s); no arbitrary selection is made — split the manifest per artifact",
				len(selected), strings.Join(names, ", ")))
	default:
		// Valid MT-001 document, but nothing KeenWhite can execute: reported,
		// not an error — the mapping simply has no runnable result.
	}

	// Capability axes: socks_tcp and socks_udp are the Provider v1 TCP/UDP
	// axes; every other capability is preserved verbatim only. A missing
	// axis is UNKNOWN (never PASS) and blocks a runnable manifest.
	for _, axis := range []string{"socks_tcp", "socks_udp"} {
		dst := &out.TCP
		if axis == "socks_udp" {
			dst = &out.UDP
		}
		cap, ok := d.Capabilities[axis]
		if !ok {
			*dst = EvidenceUnknown
			add(SeverityWarning, "MT_AXIS_MISSING", "capabilities."+axis,
				"MT document does not state this axis; mapped to UNKNOWN, manifest is not runnable")
			continue
		}
		level, valid := mtCapabilityEvidence(cap)
		if !valid {
			add(SeverityError, "MT_CAPABILITY_INVARIANT", "capabilities."+axis,
				fmt.Sprintf("capability cannot be mapped: code=%q build=%q (monotonic invariant violation)", cap.Code, cap.Build))
			continue
		}
		*dst = level
	}

	// Metadata with no Provider v1 home is carried verbatim above and
	// reported here (the difference ledger, unchanged codes from task #13).
	if d.AdapterVersion != "" {
		add(SeverityWarning, "MT_ADAPTER_VERSION", "adapter_version",
			"MT adapter_version has no Provider-v1 field; carried in Map.AdapterVersion")
	}
	if d.ServerWire != "" {
		add(SeverityWarning, "MT_SERVER_WIRE", "server_wire",
			"MT server_wire lives in the KeenWhite catalog (wire), not in the v1 manifest; carried in Map.ServerWire")
	}
	if d.ServerClass != "" {
		add(SeverityWarning, "MT_SERVER_CLASS", "server_class",
			"MT server_class lives in the KeenWhite catalog (server_class), not in the v1 manifest; carried in Map.ServerClass")
	}
	if d.ArtifactProvenance.SourceCommit != "" {
		add(SeverityWarning, "MT_SOURCE_COMMIT", "artifact_provenance.source_commit",
			"MT source_commit has no Provider-v1 field; carried in Map.SourceCommit")
	}
	if d.ResourceRequirements.Measured {
		add(SeverityUnknown, "MT_MEASURED_FLAG", "resource_requirements.measured",
			"measured=true refers to KAS lab data; KeenWhite re-measures at Prepare/Start")
	}

	// Evidence pass through the shared fail-closed rule. MT-001 static data
	// can never produce E2E_VERIFIED, so this never downgrades today; it
	// keeps the single-source-of-truth guarantee if the mapping changes.
	eff, ef := CheckEvidence(out.TCP, "", nil, 0, now)
	out.TCP = eff
	f = append(f, ef...)
	eff, ef = CheckEvidence(out.UDP, "", nil, 0, now)
	out.UDP = eff
	f = append(f, ef...)

	// KeenWhite v1 admission is stricter than MT-001 (ports 1024..65535).
	// The MT document is still valid; the difference is reported, not hidden.
	if port, err := parseMTBind(d.InboundBind); err == nil && (port < 1024) {
		add(SeverityWarning, "MT_BIND_OUTSIDE_V1", "inbound_bind",
			fmt.Sprintf("port %d is valid MT-001 but below KeenWhite v1 admission (1024..65535)", port))
	}
	return out, f
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ParseMTProviderManifest is the compatibility entry point: it returns the
// Provider v1 mapping plus findings, with a non-nil error exactly when no
// executable manifest may be constructed (parse refusal, schema violations,
// or an unmappable document). Use ParseMTDocument for the full three-level
// diagnostic.
func ParseMTProviderManifest(doc []byte, now time.Time) (MappedProviderV1, []Finding, error) {
	conv, err := ParseMTDocument(doc, now)
	if err != nil {
		return MappedProviderV1{}, conv.Findings, err
	}
	if !conv.Runnable {
		return conv.Map, conv.Findings, fmt.Errorf(
			"MT document is not convertible to an executable Provider v1 manifest (%d finding(s), runnable=false)",
			len(conv.Findings))
	}
	return conv.Map, conv.Findings, nil
}

// LoadMTProviderManifestFile reads an MT-001 manifest from disk.
func LoadMTProviderManifestFile(path string, now time.Time) (MappedProviderV1, []Finding, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return MappedProviderV1{}, nil, fmt.Errorf("read MT manifest: %w", err)
	}
	return ParseMTProviderManifest(b, now)
}
