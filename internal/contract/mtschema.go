package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// mtDoc is the MT-001 provider manifest shape (docs/research/multitransport/
// schemas/provider-v0.schema.json, schema_version const 1). Field order and
// optionality mirror the imported schema; unknown fields are rejected so a
// newer MT document cannot be interpreted silently.
type mtDoc struct {
	SchemaVersion          int            `json:"schema_version"`
	ProviderID             string         `json:"provider_id"`
	AdapterVersion         string         `json:"adapter_version"`
	ServerWire             string         `json:"server_wire"`
	ServerClass            string         `json:"server_class"`
	Architecture           string         `json:"architecture"`
	Capabilities           map[string]any `json:"capabilities"`
	InboundBind            string         `json:"inbound_bind"`
	ResourceRequirements   *mtResourceReq `json:"resource_requirements"`
	ArtifactProvenance     mtProvenance   `json:"artifact_provenance"`
	ConfigurationReference string         `json:"configuration_reference"`
}

type mtResourceReq struct {
	AdmissionMinMB  int     `json:"admission_min_mb"`
	AdmissionSource string  `json:"admission_source"`
	Measured        bool    `json:"measured"`
	MeasurementRef  *string `json:"measurement_ref"`
}

type mtProvenance struct {
	SHA256       string `json:"sha256"`
	SourceCommit string `json:"source_commit"`
	BuildRef     string `json:"build_ref,omitempty"`
	ArtifactPath string `json:"artifact_path"`
}

func parseMTDoc(doc []byte) (mtDoc, error) {
	var m mtDoc
	dec := json.NewDecoder(io.LimitReader(bytes.NewReader(doc), 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return mtDoc{}, fmt.Errorf("decode MT manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return mtDoc{}, fmt.Errorf("unexpected trailing JSON in MT manifest")
		}
		return mtDoc{}, fmt.Errorf("invalid trailing MT input: %w", err)
	}
	return m, nil
}

// LoadMTProviderManifestFile reads an MT-001 manifest from disk.
func LoadMTProviderManifestFile(path string, now time.Time) (MappedProviderV1, []Finding, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return MappedProviderV1{}, nil, fmt.Errorf("read MT manifest: %w", err)
	}
	return ParseMTProviderManifest(b, now)
}

// capLevel extracts the `code` static level of one capability axis from the
// raw MT capabilities map. MT-001 shapes capabilities as
// {name: {code, build}}; a missing axis yields "" (reported by the caller).
func capLevel(caps map[string]any, axis string) string {
	if caps == nil {
		return ""
	}
	raw, ok := caps[axis]
	if !ok {
		return ""
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	code, _ := obj["code"].(string)
	return code
}
