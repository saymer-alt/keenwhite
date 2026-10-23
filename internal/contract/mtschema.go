package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"
)

// This file mirrors docs/research/multitransport/schemas/provider-v0.schema.json
// (MT-001, schema_version const 1). The Go types and validateMTDoc implement
// the JSON Schema faithfully — a successful unmarshal alone is never treated
// as validity (task 14 stage A). The schema file itself stays the authority;
// tests cross-check both against the original fixtures.

// MTMaxDocBytes bounds one MT-001 manifest.
const MTMaxDocBytes = 64 * 1024

// MTSchemaVersion is the only MT-001 provider schema version mapped here.
const MTSchemaVersion = 1

// MTArchitecture is one verbatim architecture+ABI entry of the MT-001
// architecture array.
type MTArchitecture struct {
	Arch string `json:"arch"`
	ABI  string `json:"abi"`
}

// MTCapability is one verbatim static capability: code/build are TWO
// independent static attributes (sources vs built binary). They are never
// runtime evidence.
type MTCapability struct {
	Code  string `json:"code"`
	Build string `json:"build"`
}

// MTResourceRequirements keeps the MT-001 resource block verbatim, including
// the null-vs-integer distinction of admission_min_mb.
type MTResourceRequirements struct {
	AdmissionMinMB  *int    // nil = JSON null = no documented floor
	AdmissionSource *string // nil = JSON null
	Measured        bool
	MeasurementRef  *string // nil = JSON null
	Notes           string
}

// MTProvenance keeps the MT-001 artifact_provenance block verbatim.
type MTProvenance struct {
	SHA256       string
	SourceCommit string
	BuildRef     string // optional in MT-001
	ArtifactPath string
}

// MTDocument is the full-fidelity view of a schema-valid MT-001 provider
// manifest. Nothing is dropped: every schema field is carried here even when
// the Provider v1 mapping cannot use it.
type MTDocument struct {
	SchemaVersion          int
	ProviderID             string
	AdapterVersion         string
	ServerWire             string
	ServerClass            string
	Architecture           []MTArchitecture
	Capabilities           map[string]MTCapability
	InboundBind            string
	ResourceRequirements   MTResourceRequirements
	ArtifactProvenance     MTProvenance
	ConfigurationReference string
}

// SortedCapabilityNames returns capability names in deterministic order.
func (d MTDocument) SortedCapabilityNames() []string {
	names := make([]string, 0, len(d.Capabilities))
	for name := range d.Capabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Wire types for strict decoding. Required object fields are pointers so a
// missing member is distinguishable from an empty one.
type mtWire struct {
	SchemaVersion          int                  `json:"schema_version"`
	ProviderID             string               `json:"provider_id"`
	AdapterVersion         string               `json:"adapter_version"`
	ServerWire             string               `json:"server_wire"`
	ServerClass            string               `json:"server_class"`
	Architecture           []mtArchWire         `json:"architecture"`
	Capabilities           map[string]mtCapWire `json:"capabilities"`
	InboundBind            string               `json:"inbound_bind"`
	ResourceRequirements   *mtResourceWire      `json:"resource_requirements"`
	ArtifactProvenance     *mtProvenanceWire    `json:"artifact_provenance"`
	ConfigurationReference string               `json:"configuration_reference"`
}

type mtArchWire struct {
	Arch string `json:"arch"`
	ABI  string `json:"abi"`
}

type mtCapWire struct {
	Code  string `json:"code"`
	Build string `json:"build"`
}

type mtResourceWire struct {
	AdmissionMinMB  *int    `json:"admission_min_mb"`
	AdmissionSource *string `json:"admission_source"`
	Measured        *bool   `json:"measured"`
	MeasurementRef  *string `json:"measurement_ref"`
	Notes           *string `json:"notes"`
}

type mtProvenanceWire struct {
	SHA256       string `json:"sha256"`
	SourceCommit string `json:"source_commit"`
	BuildRef     string `json:"build_ref"`
	ArtifactPath string `json:"artifact_path"`
}

// --- schema constants (verbatim from provider-v0.schema.json) ---

var (
	mtAdapterVersionRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)
	mtServerWireRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9._/@-]{2,63}$`)
	mtServerClassRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
	mtArchABIRE        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)
	mtCapNameRE        = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
	mtArtifactPathRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	mtSourceCommitRE   = regexp.MustCompile(`^[a-f0-9]{7,64}$`)
)

var mtArchValues = map[string]bool{
	"aarch64": true, "armv7": true, "mips": true, "mipsel": true, "x86_64": true,
}

var mtStaticLevels = map[string]bool{
	"PRESENT": true, "ABSENT": true, "UNKNOWN": true,
}

// MTBuildRefForbiddenWires: provider-v0 forbids placeholder wire identifiers.
var mtForbiddenWires = map[string]bool{
	"auto": true, "any": true, "unknown": true, "n/a": true, "none": true,
}

// MTArchToKeenWhite is the ONLY documented architecture name mapping
// (task 14 A2): MT-001 Go/ELF naming to KeenWhite artifact naming.
// Architectures absent here are preserved verbatim in diagnostics and never
// silently mapped to something else.
var MTArchToKeenWhite = map[string]string{
	"aarch64": "arm64",
	"mipsel":  "mipsle",
}

// parseMTWire decodes the raw document with unknown-field and size guards.
func parseMTWire(doc []byte) (mtWire, error) {
	var w mtWire
	dec := json.NewDecoder(io.LimitReader(bytes.NewReader(doc), MTMaxDocBytes+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return mtWire{}, fmt.Errorf("decode MT manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return mtWire{}, fmt.Errorf("unexpected trailing JSON in MT manifest")
		}
		return mtWire{}, fmt.Errorf("invalid trailing MT input: %w", err)
	}
	return w, nil
}

// validateMTDoc applies every provider-v0.schema.json rule and returns the
// full-fidelity document plus one ERROR finding per violation.
func validateMTDoc(w mtWire) (MTDocument, []Finding) {
	var f []Finding
	add := func(field, msg string) {
		f = append(f, Finding{SeverityError, "MT_SCHEMA", field, msg})
	}
	var d MTDocument
	d.SchemaVersion = w.SchemaVersion
	d.ProviderID = w.ProviderID
	d.AdapterVersion = w.AdapterVersion
	d.ServerWire = w.ServerWire
	d.ServerClass = w.ServerClass
	d.InboundBind = w.InboundBind
	d.ConfigurationReference = w.ConfigurationReference

	if !providerIDRE.MatchString(w.ProviderID) {
		add("provider_id", fmt.Sprintf("must match ^[a-z][a-z0-9-]{1,31}$, got %q", w.ProviderID))
	}
	if !mtAdapterVersionRE.MatchString(w.AdapterVersion) {
		add("adapter_version", fmt.Sprintf("must be semantic version, got %q", w.AdapterVersion))
	}
	if !mtServerWireRE.MatchString(w.ServerWire) || mtForbiddenWires[w.ServerWire] {
		add("server_wire", fmt.Sprintf("must be an explicit wire identifier (placeholders forbidden), got %q", w.ServerWire))
	}
	if !mtServerClassRE.MatchString(w.ServerClass) {
		add("server_class", fmt.Sprintf("must match ^[a-z0-9][a-z0-9._-]{2,63}$, got %q", w.ServerClass))
	}

	// architecture: array of {arch, abi}, 1..8 unique items (required).
	if w.Architecture == nil {
		add("architecture", "required array is missing")
	} else if len(w.Architecture) == 0 {
		add("architecture", "must contain at least one entry")
	} else if len(w.Architecture) > 8 {
		add("architecture", fmt.Sprintf("at most 8 entries, got %d", len(w.Architecture)))
	} else {
		seen := map[MTArchitecture]bool{}
		for i, a := range w.Architecture {
			entry := MTArchitecture{Arch: a.Arch, ABI: a.ABI}
			d.Architecture = append(d.Architecture, entry)
			field := fmt.Sprintf("architecture[%d]", i)
			if !mtArchValues[a.Arch] {
				add(field+".arch", fmt.Sprintf("must be one of aarch64|armv7|mips|mipsel|x86_64, got %q", a.Arch))
			}
			if !mtArchABIRE.MatchString(a.ABI) {
				add(field+".abi", fmt.Sprintf("must match ^[a-z0-9][a-z0-9._-]{1,31}$, got %q", a.ABI))
			}
			if seen[entry] {
				add(field, fmt.Sprintf("duplicate architecture entry %s/%s", a.Arch, a.ABI))
			}
			seen[entry] = true
		}
	}

	// capabilities: 1+ entries, static levels only, monotonic invariant
	// build=PRESENT ⇒ code=PRESENT (schema allOf).
	if w.Capabilities == nil {
		add("capabilities", "required object is missing")
	} else if len(w.Capabilities) == 0 {
		add("capabilities", "must contain at least one capability")
	} else {
		d.Capabilities = make(map[string]MTCapability, len(w.Capabilities))
		for _, name := range sortedWireCaps(w.Capabilities) {
			cap := w.Capabilities[name]
			field := "capabilities." + name
			d.Capabilities[name] = MTCapability{Code: cap.Code, Build: cap.Build}
			if !mtCapNameRE.MatchString(name) {
				add(field, fmt.Sprintf("capability name must match ^[a-z][a-z0-9_]{1,31}$, got %q", name))
			}
			if !mtStaticLevels[cap.Code] {
				add(field+".code", fmt.Sprintf("static level must be PRESENT|ABSENT|UNKNOWN, got %q", cap.Code))
			}
			if !mtStaticLevels[cap.Build] {
				add(field+".build", fmt.Sprintf("static level must be PRESENT|ABSENT|UNKNOWN, got %q", cap.Build))
			}
			if cap.Build == "PRESENT" && cap.Code != "PRESENT" {
				add(field, fmt.Sprintf("monotonic invariant violated: build=PRESENT requires code=PRESENT, got code=%q", cap.Code))
			}
		}
	}

	// inbound_bind: exactly 127.0.0.1:port, port 1..65535 (MT-001 loopback rule).
	if port, err := parseMTBind(w.InboundBind); err != nil {
		add("inbound_bind", err.Error())
	} else if port < 1 || port > 65535 {
		add("inbound_bind", fmt.Sprintf("port must be 1..65535, got %d", port))
	}

	// resource_requirements: required object, conditional constraints.
	if w.ResourceRequirements == nil {
		add("resource_requirements", "required object is missing")
	} else {
		r := w.ResourceRequirements
		d.ResourceRequirements = MTResourceRequirements{
			AdmissionMinMB:  r.AdmissionMinMB,
			AdmissionSource: r.AdmissionSource,
			MeasurementRef:  r.MeasurementRef,
		}
		if r.Measured == nil {
			add("resource_requirements.measured", "required boolean is missing")
		} else {
			d.ResourceRequirements.Measured = *r.Measured
		}
		if r.AdmissionMinMB != nil && (*r.AdmissionMinMB < 64 || *r.AdmissionMinMB > 16384) {
			add("resource_requirements.admission_min_mb", fmt.Sprintf("integer floor must be 64..16384, got %d", *r.AdmissionMinMB))
		}
		if r.AdmissionSource != nil && (len(*r.AdmissionSource) < 4 || len(*r.AdmissionSource) > 128) {
			add("resource_requirements.admission_source", "must be 4..128 characters when present")
		}
		if r.AdmissionMinMB != nil && (r.AdmissionSource == nil || len(*r.AdmissionSource) < 4) {
			add("resource_requirements.admission_source", "required (min 4 chars) when admission_min_mb is an integer")
		}
		if r.Measured != nil && *r.Measured && (r.MeasurementRef == nil || len(*r.MeasurementRef) < 4) {
			add("resource_requirements.measurement_ref", "required (min 4 chars) when measured is true")
		}
		if r.MeasurementRef != nil && (len(*r.MeasurementRef) < 4 || len(*r.MeasurementRef) > 256) {
			add("resource_requirements.measurement_ref", "must be 4..256 characters when present")
		}
		if r.Notes != nil {
			if len(*r.Notes) > 512 {
				add("resource_requirements.notes", "must be at most 512 characters")
			}
			d.ResourceRequirements.Notes = *r.Notes
		}
	}

	// artifact_provenance: required object.
	if w.ArtifactProvenance == nil {
		add("artifact_provenance", "required object is missing")
	} else {
		p := w.ArtifactProvenance
		d.ArtifactProvenance = MTProvenance{
			SHA256: p.SHA256, SourceCommit: p.SourceCommit,
			BuildRef: p.BuildRef, ArtifactPath: p.ArtifactPath,
		}
		if !sha256RE.MatchString(p.SHA256) {
			add("artifact_provenance.sha256", "must be exactly 64 lowercase hex characters")
		}
		if !mtSourceCommitRE.MatchString(p.SourceCommit) {
			add("artifact_provenance.source_commit", "must be 7..64 lowercase hex characters")
		}
		if p.BuildRef != "" && (len(p.BuildRef) < 4 || len(p.BuildRef) > 256) {
			add("artifact_provenance.build_ref", "must be 4..256 characters when present")
		}
		if !mtArtifactPathRE.MatchString(p.ArtifactPath) || hasParentSegment(p.ArtifactPath) {
			add("artifact_provenance.artifact_path", fmt.Sprintf("must be a provider-relative path without traversal, got %q", p.ArtifactPath))
		}
	}

	if len(w.ConfigurationReference) < 4 || len(w.ConfigurationReference) > 256 {
		add("configuration_reference", "must be 4..256 characters")
	}
	return d, f
}

func sortedWireCaps(m map[string]mtCapWire) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func hasParentSegment(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// parseMTBind implements the MT-001 loopbackBind rule: exactly 127.0.0.1 and
// a decimal port 1..65535. Wildcards, LAN, IPv6, "localhost" and other 127/8
// aliases are rejected.
func parseMTBind(addr string) (int, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("invalid bind; expected 127.0.0.1:port")
	}
	if host != "127.0.0.1" {
		return 0, fmt.Errorf("bind must be exactly 127.0.0.1, got %q", host)
	}
	port := 0
	for _, r := range portText {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("bind port must be numeric, got %q", portText)
		}
		port = port*10 + int(r-'0')
		if port > 65535 {
			return 0, fmt.Errorf("bind port must be 1..65535, got %q", portText)
		}
	}
	if port < 1 {
		return 0, fmt.Errorf("bind port must be 1..65535, got %q", portText)
	}
	if len(portText) > 1 && portText[0] == '0' {
		return 0, fmt.Errorf("bind port must not have leading zeros, got %q", portText)
	}
	return port, nil
}

// ParseMTDocument is the full-fidelity MT-001 entry point (task 14 A2):
// it separates (1) parsing the source document, (2) partial conversion and
// (3) a safely executable Provider v1 manifest into three distinct levels —
// Parsed, Partial and Runnable on the returned MTConversion.
//
// A Go error is returned only when the document cannot be interpreted at
// all: oversize, undecodable JSON, trailing input, or an unknown
// schema_version. Every schema violation is instead reported as ERROR
// findings with Parsed=false.
func ParseMTDocument(doc []byte, now time.Time) (MTConversion, error) {
	var conv MTConversion
	w, err := parseMTWire(doc)
	if err != nil {
		return conv, err
	}
	if w.SchemaVersion != MTSchemaVersion {
		return conv, fmt.Errorf("%w: schema_version %d, want %d",
			ErrUnknownSchemaVersion, w.SchemaVersion, MTSchemaVersion)
	}
	d, f := validateMTDoc(w)
	conv.Document = d
	conv.Findings = f
	conv.Parsed = len(f) == 0

	conv.Map, conv.Findings = mapMTToProviderV1(d, conv.Findings, now)
	conv.Partial = false
	for _, x := range conv.Findings {
		if x.Severity != SeverityError {
			conv.Partial = true
			break
		}
	}
	// Runnable requires: schema-valid document, no ERROR findings, exactly
	// one executable architecture, both capability axes stated, and neither
	// axis mapped to UNSUPPORTED (an artifact that statically lacks the
	// feature cannot back an executable manifest for it; UNKNOWN stays
	// structurally runnable and fails closed at runtime admission).
	_, tcpStated := d.Capabilities["socks_tcp"]
	_, udpStated := d.Capabilities["socks_udp"]
	conv.Runnable = conv.Parsed && conv.Map.Arch != "" && tcpStated && udpStated &&
		conv.Map.TCP != EvidenceUnsupported && conv.Map.UDP != EvidenceUnsupported
	for _, x := range conv.Findings {
		if x.Severity == SeverityError {
			conv.Runnable = false
			break
		}
	}
	return conv, nil
}

// MTConversion is the typed result of the three conversion levels (A2).
type MTConversion struct {
	Document MTDocument       // full-fidelity source view (valid when Parsed)
	Map      MappedProviderV1 // Provider v1 projection (may be partial)
	Findings []Finding        // every schema violation and every mapping difference
	Parsed   bool             // document conforms to provider-v0.schema.json v1
	Partial  bool             // at least one mapping difference (WARNING or softer)
	Runnable bool             // safe to construct an executable Provider v1 manifest
}
