// Package catalog validates KeenWhite's offline provider registry.
// It intentionally does not execute external transports, open sockets, or read secrets.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"time"
)

const APIVersion = "keenwhite.dev/v0alpha1"
const maxCatalogBytes = 1024 * 1024

var providerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

type Catalog struct {
	APIVersion string     `json:"api_version"`
	Providers  []Provider `json:"providers"`
}

type Provider struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Lifecycle           string `json:"lifecycle"`
	Source              string `json:"source"`
	Wire                string `json:"wire"`
	ServerClass         string `json:"server_class"`
	Bind                string `json:"bind"`
	TCP                 string `json:"tcp"`
	UDP                 string `json:"udp"`
	KASCombinedMinRAMMB *int   `json:"kas_combined_min_ram_mb"`
	Notes               string `json:"notes,omitempty"`
	EvidenceURL         string `json:"evidence_url,omitempty"`
	// EvidenceMeasuredAt is optional: the ORIGINAL date of the measurement
	// backing an E2E_VERIFIED claim. Absent = WARNING (not a hard error, so
	// pre-0.2.1 catalogs stay valid).
	EvidenceMeasuredAt *time.Time `json:"evidence_measured_at,omitempty"`
}

// Load only reads up to one megabyte and forbids unknown fields and trailing JSON.
func Load(path string) (Catalog, error) {
	var out Catalog
	f, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf("open catalog: %w", err)
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxCatalogBytes+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Catalog{}, errors.New("unexpected trailing JSON")
		}
		return Catalog{}, fmt.Errorf("invalid trailing input: %w", err)
	}
	return out, nil
}

var lifecycleValues = map[string]bool{"research": true, "candidate": true, "lab": true}
var capabilityValues = map[string]bool{"UNKNOWN": true, "UNSUPPORTED": true, "SOURCE_CONFIRMED": true, "BUILD_CONFIRMED": true, "E2E_VERIFIED": true}

// Validate implements a strict admission *documentation* gate, not a runtime
// health check. It is the historical string projection of ValidateDetailed
// (diagnostics.go) — both severities are included; the CLI and other
// callers that must separate blocking from advisory findings use
// ValidateDetailed (issue #32).
func Validate(c Catalog) []string {
	findings, _ := ValidateDetailed(c, time.Now())
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Field+": "+f.Message)
	}
	return out
}

// ValidateBind allows ONLY the v0 IPv4 loopback address and nonprivileged ports.
// It deliberately rejects localhost, wildcard binds, other 127/8 aliases, and IPv6.
func ValidateBind(addr string) error {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("invalid bind; expected 127.0.0.1:port")
	}
	if host != "127.0.0.1" {
		return errors.New("bind must be 127.0.0.1 only")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1024 || port > 65535 {
		return errors.New("bind port must be 1024..65535")
	}
	return nil
}
