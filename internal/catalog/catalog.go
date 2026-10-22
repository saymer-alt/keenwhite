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
	"strings"
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

// Validate implements a strict admission *documentation* gate, not a runtime health check.
func Validate(c Catalog) []string {
	var problems []string
	if c.APIVersion != APIVersion {
		problems = append(problems, "api_version must be "+APIVersion)
	}
	if len(c.Providers) == 0 {
		problems = append(problems, "providers must not be empty")
	}
	if len(c.Providers) > 100 {
		problems = append(problems, "too many providers")
	}
	ids := make(map[string]bool)
	binds := make(map[string]string)
	for i, p := range c.Providers {
		prefix := fmt.Sprintf("providers[%d] (%s): ", i, p.ID)
		if !providerIDPattern.MatchString(p.ID) {
			problems = append(problems, prefix+"invalid ID")
		}
		if ids[p.ID] {
			problems = append(problems, prefix+"duplicate ID")
		}
		ids[p.ID] = true
		if strings.TrimSpace(p.Name) == "" {
			problems = append(problems, prefix+"name required")
		}
		if !lifecycleValues[p.Lifecycle] {
			problems = append(problems, prefix+"invalid lifecycle (no production state in alpha)")
		}
		if p.Source == "" || !strings.HasPrefix(p.Source, "https://github.com/") {
			problems = append(problems, prefix+"source must be a GitHub HTTPS URL")
		}
		if p.Wire == "" || p.ServerClass == "" {
			problems = append(problems, prefix+"wire and server_class required")
		}
		if !capabilityValues[p.TCP] {
			problems = append(problems, prefix+"invalid TCP evidence level")
		}
		if !capabilityValues[p.UDP] {
			problems = append(problems, prefix+"invalid UDP evidence level")
		}
		if (p.TCP == "E2E_VERIFIED" || p.UDP == "E2E_VERIFIED") && p.EvidenceURL == "" {
			problems = append(problems, prefix+"E2E_VERIFIED requires evidence_url")
		}
		if p.KASCombinedMinRAMMB != nil && (*p.KASCombinedMinRAMMB < 64 || *p.KASCombinedMinRAMMB > 16384) {
			problems = append(problems, prefix+"invalid combined RAM floor")
		}
		if err := ValidateBind(p.Bind); err != nil {
			problems = append(problems, prefix+err.Error())
		}
		if prior, ok := binds[p.Bind]; ok {
			problems = append(problems, prefix+"bind conflicts with "+prior)
		}
		binds[p.Bind] = p.ID
	}
	return problems
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
