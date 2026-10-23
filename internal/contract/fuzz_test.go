package contract

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseMTDocument protects the MT-001 parser (task 14 stage E): for ANY
// input, the three-level conversion must stay internally consistent —
// Runnable implies Parsed, an unparsed document always carries findings,
// and MT-001 static data can never produce E2E_VERIFIED.
func FuzzParseMTDocument(f *testing.F) {
	// Seeds: the real fixtures plus the minimal synthetic document.
	for _, rel := range []string{
		"valid/wdtt-manifest.json", "valid/openflux-manifest.json",
		"invalid/bad-capability-value.json", "invalid/contradictory-capability.json",
		"invalid/non-loopback-bind.json", "invalid/wrong-schema-version.json",
	} {
		b, err := os.ReadFile(filepath.Join(mtFixtureDir, rel))
		if err == nil {
			f.Add(b)
		}
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"schema_version":1}`))
	seed, _ := mtMinimalWire(nil) // marshal of a plain map cannot fail
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		conv, err := ParseMTDocument(data, now)
		if err != nil {
			return // hard refusal: oversize, JSON syntax, unknown version
		}
		if conv.Runnable && !conv.Parsed {
			t.Fatal("Runnable without Parsed")
		}
		if !conv.Parsed && len(conv.Findings) == 0 {
			t.Fatal("unparsed document must carry findings")
		}
		if conv.Map.TCP == EvidenceE2EVerified || conv.Map.UDP == EvidenceE2EVerified {
			t.Fatal("MT-001 static data must never produce E2E_VERIFIED")
		}
		if conv.Partial && conv.Runnable && len(conv.Findings) == 0 {
			t.Fatal("partial conversion must be explained by findings")
		}
	})
}
