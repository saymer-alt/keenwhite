package provider

import "testing"

// Guard the wire constant: every adapter reports exactly this version.
func TestAPIVersionConstant(t *testing.T) {
	if APIVersionV1 != "keenwhite.provider/v1-PROOF-RED" {
		t.Fatalf("APIVersionV1 drifted: %q", APIVersionV1)
	}
}
