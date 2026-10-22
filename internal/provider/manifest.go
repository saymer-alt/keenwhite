package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/saymer-alt/keenwhite/internal/catalog"
)

// ManifestVersion is the only manifest major version this build accepts.
const ManifestVersion = 1

// DefaultMeasurementTTL bounds how long an E2E measurement may back an
// E2E_VERIFIED claim. After it expires the claim degrades to UNKNOWN.
const DefaultMeasurementTTL = 90 * 24 * time.Hour

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var archValues = map[string]bool{"arm64": true, "mipsle": true}

// Manifest is the versioned provider manifest (v1). It is intentionally
// small: the catalog (examples/providers.json) stays the cross-provider
// registry; a manifest describes ONE provider's runnable specifics.
// Unknown fields are rejected so old/new documents cannot silently drift.
type Manifest struct {
	ManifestVersion int              `json:"manifest_version"`
	APIVersion      string           `json:"api_version"`
	Provider        string           `json:"provider"`
	Artifact        ManifestArtifact `json:"artifact"`
	Profile         ManifestProfile  `json:"profile"`
	Socks           ManifestSocks    `json:"socks"`
	Resources       ManifestRes      `json:"resources"`
	Lifecycle       ManifestLife     `json:"lifecycle"`
	Evidence        ManifestEvid     `json:"evidence"`
}

type ManifestArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Arch   string `json:"arch"`
}

type ManifestProfile struct {
	Path          string   `json:"path"`
	RequiredFiles []string `json:"required_files,omitempty"`
}

type ManifestSocks struct {
	Bind string   `json:"bind"`
	TCP  Evidence `json:"tcp"`
	UDP  Evidence `json:"udp"`
}

type ManifestRes struct {
	MinMemAvailableMB int `json:"min_mem_available_mb,omitempty"`
	// KASCombinedMinRAMMB is documentation-only in KeenWhite: the
	// KAS+WDTT ≥512 MB admission policy belongs to KAS (KAS #137) and is
	// never applied globally to KeenWhite or other providers.
	KASCombinedMinRAMMB *int   `json:"kas_combined_min_ram_mb,omitempty"`
	AdmissionSource     string `json:"admission_source,omitempty"`
}

type ManifestLife struct {
	HardStopSeconds  int `json:"hard_stop_seconds"`
	StopGraceSeconds int `json:"stop_grace_seconds,omitempty"`
}

type ManifestEvid struct {
	EvidenceURL string     `json:"evidence_url,omitempty"`
	MeasuredAt  *time.Time `json:"measured_at,omitempty"`
	// MeasurementTTLSeconds overrides DefaultMeasurementTTL when > 0.
	MeasurementTTLSeconds int `json:"measurement_ttl_seconds,omitempty"`
}

// LoadManifest reads and structurally validates a v1 manifest file.
// It rejects unknown fields, trailing JSON and documents larger than 64 KiB.
func LoadManifest(path string) (Manifest, []Problem, error) {
	var m Manifest
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, nil, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Manifest{}, nil, fmt.Errorf("unexpected trailing JSON in manifest")
		}
		return Manifest{}, nil, fmt.Errorf("invalid trailing manifest input: %w", err)
	}
	problems := ValidateManifest(m)
	for _, p := range problems {
		if p.Severity == "error" {
			return m, problems, fmt.Errorf("manifest validation failed: %s: %s", p.Field, p.Message)
		}
	}
	return m, problems, nil
}

// ValidateManifest applies the v1 semantic rules. Errors block admission;
// warnings are advisory (and tests rely on exact field names).
func ValidateManifest(m Manifest) []Problem {
	var p []Problem
	if m.ManifestVersion != ManifestVersion {
		p = append(p, Errorf("manifest_version",
			"only manifest_version %d is supported by this build", ManifestVersion))
	}
	if m.APIVersion != APIVersionV1 {
		p = append(p, Errorf("api_version", "api_version must be %s", APIVersionV1))
	}
	if len(m.Provider) < 2 || len(m.Provider) > 32 {
		p = append(p, Errorf("provider", "provider id required (2..32 chars)"))
	}
	if m.Artifact.Path == "" {
		p = append(p, Errorf("artifact.path", "artifact path required"))
	}
	if !sha256Pattern.MatchString(m.Artifact.SHA256) {
		p = append(p, Errorf("artifact.sha256",
			"must be exactly 64 lowercase hex characters"))
	}
	if !archValues[m.Artifact.Arch] {
		p = append(p, Errorf("artifact.arch", "arch must be arm64 or mipsle"))
	}
	if m.Profile.Path == "" {
		p = append(p, Errorf("profile.path", "profile path required"))
	}
	if err := catalog.ValidateBind(m.Socks.Bind); err != nil {
		p = append(p, Errorf("socks.bind", "%v", err))
	}
	if !m.Socks.TCP.Valid() {
		p = append(p, Errorf("socks.tcp", "invalid evidence level %q", m.Socks.TCP))
	}
	if !m.Socks.UDP.Valid() {
		p = append(p, Errorf("socks.udp", "invalid evidence level %q", m.Socks.UDP))
	}
	// E2E claims need a fresh, referenced measurement (MT-002 semantics:
	// stale results cannot claim PASS, and pid/listener facts are not E2E).
	if m.Socks.TCP == EvidenceE2EVerified || m.Socks.UDP == EvidenceE2EVerified {
		if m.Evidence.EvidenceURL == "" {
			p = append(p, Errorf("evidence.evidence_url",
				"E2E_VERIFIED requires evidence_url"))
		}
		if m.Evidence.MeasuredAt == nil {
			p = append(p, Errorf("evidence.measured_at",
				"E2E_VERIFIED requires measured_at"))
		}
	}
	if m.Evidence.MeasuredAt != nil {
		ttl := DefaultMeasurementTTL
		if m.Evidence.MeasurementTTLSeconds > 0 {
			ttl = time.Duration(m.Evidence.MeasurementTTLSeconds) * time.Second
		}
		if time.Since(*m.Evidence.MeasuredAt) > ttl {
			// Downgrade, not silence: stale measurement cannot back E2E.
			if m.Socks.TCP == EvidenceE2EVerified || m.Socks.UDP == EvidenceE2EVerified {
				p = append(p, Errorf("evidence.measured_at",
					"measurement is older than its TTL (%s); stale results cannot claim E2E_VERIFIED", ttl))
			} else {
				p = append(p, Warnf("evidence.measured_at", "measurement is older than its TTL (%s)", ttl))
			}
		}
	}
	if m.Lifecycle.HardStopSeconds <= 0 {
		p = append(p, Errorf("lifecycle.hard_stop_seconds",
			"a positive hard time limit is mandatory (WDTT: exactly 1800)"))
	}
	if m.Lifecycle.StopGraceSeconds < 0 || m.Lifecycle.StopGraceSeconds > 300 {
		p = append(p, Errorf("lifecycle.stop_grace_seconds", "must be 0..300"))
	}
	if m.Resources.MinMemAvailableMB < 0 || m.Resources.MinMemAvailableMB > 16384 {
		p = append(p, Errorf("resources.min_mem_available_mb", "must be 0..16384"))
	}
	return p
}

// MeasurementTTL returns the manifest's effective measurement TTL.
func (m Manifest) MeasurementTTL() time.Duration {
	if m.Evidence.MeasurementTTLSeconds > 0 {
		return time.Duration(m.Evidence.MeasurementTTLSeconds) * time.Second
	}
	return DefaultMeasurementTTL
}
