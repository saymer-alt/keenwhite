package catalog

import (
	"strings"
	"testing"
	"time"

	"github.com/saymer-alt/keenwhite/internal/contract"
)

func valid() Catalog {
	ram := 512
	return Catalog{APIVersion: APIVersion, Providers: []Provider{
		{ID: "wdtt", Name: "WDTT Suite", Lifecycle: "lab", Source: "https://github.com/saymer-alt/wdtt-suite", Wire: "wdtt-specific", ServerClass: "wdtt-exit", Bind: "127.0.0.1:1081", TCP: "SOURCE_CONFIRMED", UDP: "UNKNOWN", KASCombinedMinRAMMB: &ram},
	}}
}

func TestValid(t *testing.T) {
	if x := Validate(valid()); len(x) != 0 {
		t.Fatalf("unexpected problems: %v", x)
	}
}

func TestBind(t *testing.T) {
	bad := []string{"0.0.0.0:1081", ":1081", "[::1]:1081", "localhost:1081", "127.0.0.2:1081", "192.168.1.1:1081", "127.0.0.1:80", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1", "127.0.0.1:abc"}
	for _, s := range bad {
		if err := ValidateBind(s); err == nil {
			t.Errorf("accepted invalid bind %q", s)
		}
	}
	for _, s := range []string{"127.0.0.1:1024", "127.0.0.1:1081", "127.0.0.1:65535"} {
		if err := ValidateBind(s); err != nil {
			t.Errorf("rejected valid bind %q: %v", s, err)
		}
	}
}

func TestPortConflict(t *testing.T) {
	c := valid()
	p := c.Providers[0]
	p.ID = "openflux"
	c.Providers = append(c.Providers, p)
	if !contains(Validate(c), "bind conflicts") {
		t.Fatal("port collision not found")
	}
}

func TestIDDuplicate(t *testing.T) {
	c := valid()
	c.Providers = append(c.Providers, c.Providers[0])
	c.Providers[1].Bind = "127.0.0.1:1083"
	if !contains(Validate(c), "duplicate ID") {
		t.Fatal("duplicate ID not found")
	}
}

func TestE2ERequiresEvidence(t *testing.T) {
	// Issue #32 policy: a legacy E2E_VERIFIED without a URL keeps the
	// document structurally valid (WARNING, not ERROR) but its effective
	// level is downgraded to UNKNOWN — never echoed back as verified.
	c := valid()
	c.Providers[0].TCP = "E2E_VERIFIED"
	findings, effs := ValidateDetailed(c, time.Now())
	found := false
	for _, f := range findings {
		if f.Severity == contract.SeverityWarning && f.Code == "E2E_NO_URL" && strings.HasSuffix(f.Field, ".tcp") {
			found = true
		}
	}
	if !found {
		t.Fatalf("E2E_NO_URL warning required, got %+v", findings)
	}
	if len(effs) != 1 || effs[0].TCP != contract.EvidenceUnknown || effs[0].UDP != "UNKNOWN" {
		t.Fatalf("effective TCP must be downgraded to UNKNOWN: %+v", effs)
	}
	// The string projection still reports the finding (compat callers see it).
	if !contains(Validate(c), "E2E_NO_URL") && !contains(Validate(c), "evidence URL") {
		t.Fatal("string projection lost the finding")
	}
}

func TestNoProductionLifecycle(t *testing.T) {
	c := valid()
	c.Providers[0].Lifecycle = "production"
	if !contains(Validate(c), "invalid lifecycle") {
		t.Fatal("alpha accepted production state")
	}
}

func TestCapability(t *testing.T) {
	c := valid()
	c.Providers[0].UDP = "works"
	if !contains(Validate(c), "invalid UDP") {
		t.Fatal("invalid capability accepted")
	}
}

func TestInvalidRAMPolicy(t *testing.T) {
	c := valid()
	bad := 16
	c.Providers[0].KASCombinedMinRAMMB = &bad
	if !contains(Validate(c), "invalid combined RAM") {
		t.Fatal("RAM policy accepted")
	}
}

func contains(errors []string, s string) bool {
	for _, e := range errors {
		if strings.Contains(e, s) {
			return true
		}
	}
	return false
}
