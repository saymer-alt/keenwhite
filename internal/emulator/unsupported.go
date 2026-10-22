package emulator

import (
	"context"
	"fmt"
)

// UnsupportedEnvironment is the shipped fail-closed implementation: the
// emulator does not exist yet, so every operation reports UNSUPPORTED and
// nothing ever executes. It exists so the harness and CI have a real object
// behind the contract and so "no emulator connected" is an explicit,
// testable state — never a fake pass.
type UnsupportedEnvironment struct {
	// Reason is surfaced in diagnostics (non-secret).
	Reason string
}

// NewUnsupportedEnvironment builds the stub with a default reason.
func NewUnsupportedEnvironment() *UnsupportedEnvironment {
	return &UnsupportedEnvironment{Reason: "no Keenetic emulator is connected yet (task #12 stage C stub)"}
}

// DetectEnvironment reports UNSUPPORTED with a zero device.
func (e *UnsupportedEnvironment) DetectEnvironment(_ context.Context) (DeviceInfo, error) {
	return DeviceInfo{ContractAPI: APIVersionV0, EmulatorKind: "none"}, fmt.Errorf("%w: DetectEnvironment", ErrUnsupported)
}

// CheckCapabilities answers every inventory capability with UNSUPPORTED.
func (e *UnsupportedEnvironment) CheckCapabilities(_ context.Context) (map[Capability]Support, error) {
	out := make(map[Capability]Support, len(AllCapabilities))
	for _, c := range AllCapabilities {
		out[c] = Unsupported
	}
	return out, nil // answering is supported; the capabilities themselves are not
}

// ReadResources reports empty resources and UNSUPPORTED.
func (e *UnsupportedEnvironment) ReadResources(_ context.Context) (Resources, error) {
	return Resources{}, fmt.Errorf("%w: ReadResources", ErrUnsupported)
}

// StageArtifact refuses: nothing may be staged outside a real environment.
func (e *UnsupportedEnvironment) StageArtifact(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("%w: StageArtifact", ErrUnsupported)
}

// RunIsolatedTest refuses UNCONDITIONALLY: without a proven-isolated device
// there is no test to run, and this stub never executes anything.
func (e *UnsupportedEnvironment) RunIsolatedTest(_ context.Context, _ TestRequest) (TestResult, error) {
	return TestResult{}, fmt.Errorf("%w: RunIsolatedTest", ErrUnsupported)
}

// CollectDiagnostics returns a minimal, secret-free stub bundle.
func (e *UnsupportedEnvironment) CollectDiagnostics(_ context.Context) ([]byte, error) {
	return []byte(fmt.Sprintf(`{"contract_api":%q,"environment":"unsupported","reason":%q}`,
		APIVersionV0, e.Reason)), nil
}

// Cleanup is idempotent and always succeeds (nothing was started).
func (e *UnsupportedEnvironment) Cleanup(_ context.Context) error { return nil }
