package catalog

import (
	"fmt"
	"strings"
	"time"

	"github.com/saymer-alt/keenwhite/internal/contract"
)

// Structured diagnostics for the catalog (issue #32). The historical
// Validate(c) []string could not separate "invalid document" from
// "acceptable with limitations", so the CLI failed on advisory findings.
// ValidateDetailed returns typed findings with an explicit severity plus
// the per-provider EFFECTIVE evidence levels after fail-closed downgrades.

// EffectiveEvidence carries the per-axis evidence AFTER the shared
// fail-closed rule (contract.CheckEvidence). A legacy E2E_VERIFIED claim
// that cannot be re-checked (no URL, no date, stale, future-dated) is
// reported at its effective level UNKNOWN — never echoed back as verified.
type EffectiveEvidence struct {
	ProviderID string
	TCP        string
	UDP        string
}

// String renders the effective levels compactly (diagnostics and logs).
func (e EffectiveEvidence) String() string {
	return e.ProviderID + " tcp=" + e.TCP + " udp=" + e.UDP
}

// ValidateDetailed validates the catalog and returns structured findings
// plus the effective per-provider evidence.
//
// Severity policy (issue #32 acceptance):
//   - ERROR: the document is invalid (structure, enums, duplicates, bind
//     rules) — CLI exits non-zero.
//   - WARNING: the document is acceptable with limitations; an unverifiable
//     E2E_VERIFIED claim stays in the document but its EFFECTIVE level is
//     downgraded to UNKNOWN and the downgrade is reported — CLI exits zero
//     with a visible warning. This is the documented legacy-catalog policy:
//     accept, downgrade, warn — never silently elevate.
//
// Codes of evidence findings are preserved from the shared rule
// (E2E_NO_URL, E2E_NO_MEASUREMENT, E2E_FUTURE_DATE, E2E_STALE); only the
// catalog-layer severity is advisory. TCP and UDP are judged independently.
func ValidateDetailed(c Catalog, now time.Time) ([]contract.Finding, []EffectiveEvidence) {
	var findings []contract.Finding
	add := func(sev contract.Severity, code, field, msg string) {
		findings = append(findings, contract.Finding{Severity: sev, Code: code, Field: field, Message: msg})
	}

	if c.APIVersion != APIVersion {
		add(contract.SeverityError, "CATALOG_API_VERSION", "api_version", "api_version must be "+APIVersion)
	}
	if len(c.Providers) == 0 {
		add(contract.SeverityError, "EMPTY_CATALOG", "providers", "providers must not be empty")
	}
	if len(c.Providers) > 100 {
		add(contract.SeverityError, "TOO_MANY_PROVIDERS", "providers", "too many providers")
	}

	ids := make(map[string]bool)
	binds := make(map[string]string)
	effs := make([]EffectiveEvidence, 0, len(c.Providers))
	for i, p := range c.Providers {
		field := fmt.Sprintf("providers[%d] (%s)", i, p.ID)
		if !providerIDPattern.MatchString(p.ID) {
			add(contract.SeverityError, "PROVIDER_ID", field+".id", "invalid ID")
		}
		if ids[p.ID] {
			add(contract.SeverityError, "DUPLICATE_PROVIDER", field+".id", "duplicate ID")
		}
		ids[p.ID] = true
		if strings.TrimSpace(p.Name) == "" {
			add(contract.SeverityError, "PROVIDER_NAME", field+".name", "name required")
		}
		if !lifecycleValues[p.Lifecycle] {
			add(contract.SeverityError, "LIFECYCLE", field+".lifecycle", "invalid lifecycle (no production state in alpha)")
		}
		if p.Source == "" || !isGitHubHTTPS(p.Source) {
			add(contract.SeverityError, "SOURCE_URL", field+".source", "source must be a GitHub HTTPS URL")
		}
		if p.Wire == "" || p.ServerClass == "" {
			add(contract.SeverityError, "WIRE_CLASS", field+".wire", "wire and server_class required")
		}
		if !capabilityValues[p.TCP] {
			add(contract.SeverityError, "EVIDENCE_LEVEL", field+".tcp", "invalid TCP evidence level")
		}
		if !capabilityValues[p.UDP] {
			add(contract.SeverityError, "EVIDENCE_LEVEL", field+".udp", "invalid UDP evidence level")
		}
		if p.KASCombinedMinRAMMB != nil && (*p.KASCombinedMinRAMMB < 64 || *p.KASCombinedMinRAMMB > 16384) {
			add(contract.SeverityError, "RAM_FLOOR", field+".kas_combined_min_ram_mb", "invalid combined RAM floor")
		}
		if err := ValidateBind(p.Bind); err != nil {
			add(contract.SeverityError, "BIND_INVALID", field+".bind", err.Error())
		}
		if prior, ok := binds[p.Bind]; ok {
			add(contract.SeverityError, "BIND_CONFLICT", field+".bind", "bind conflicts with "+prior)
		}
		binds[p.Bind] = p.ID

		// Effective evidence: per axis, through the ONE shared rule. The
		// written level stays in the document; only the effective level is
		// downgraded, and every downgrade is a visible finding.
		eff := EffectiveEvidence{ProviderID: p.ID, TCP: p.TCP, UDP: p.UDP}
		for _, ax := range [][2]string{{"tcp", p.TCP}, {"udp", p.UDP}} {
			if ax[1] != contract.EvidenceE2EVerified {
				continue // non-E2E levels pass through (same as CheckEvidence)
			}
			level, ef := contract.CheckEvidence(ax[1], p.EvidenceURL, p.EvidenceMeasuredAt, 0, now)
			for _, f := range ef {
				// Advisory at the catalog layer: the document stays
				// structurally valid; the claim is downgraded, not echoed.
				add(contract.SeverityWarning, f.Code, field+"."+ax[0], f.Message)
			}
			if ax[0] == "tcp" {
				eff.TCP = level
			} else {
				eff.UDP = level
			}
		}
		effs = append(effs, eff)
	}
	return findings, effs
}

func isGitHubHTTPS(s string) bool {
	const prefix = "https://github.com/"
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
