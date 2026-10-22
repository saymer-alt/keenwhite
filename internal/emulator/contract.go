// Package emulator defines the preliminary contract (`keenetic-emulator/v0`)
// between KeenWhite's test harness and a future virtual Keenetic device
// (the QEMU-based "Ported 5.1.8" project). The contract is a STUB today:
// no emulator exists yet, so the only shipped implementation is
// UnsupportedEnvironment, which fails closed on every operation.
//
// Trust and safety boundaries for every future implementation:
//   - the environment is FULLY isolated from home/production networks;
//   - no real VK/MAX accounts, no real TURN credentials, no production VPS;
//   - test DNS only; no host route/DNS/firewall changes, ever;
//   - transport binaries run ONLY through RunIsolatedTest inside the
//     emulated device, never on the host;
//   - every run is time-limited and ends with Cleanup terminating all
//     children;
//   - diagnostics are secret-free by construction.
//
// Network isolation is NOT considered verified until the concrete QEMU
// configuration has itself been tested; implementations must report
// capability UNKNOWN rather than claiming isolation.
package emulator

import (
	"context"
	"errors"
	"time"
)

// APIVersionV0 is the contract version implemented by this package.
const APIVersionV0 = "keenetic-emulator/v0"

// ErrUnsupported is returned by operations the connected environment does
// not implement. Callers must surface it as UNSUPPORTED, never as PASS.
var ErrUnsupported = errors.New("unsupported by this environment (keenetic-emulator/v0 stub)")

// Support is the tri-state capability answer. Fail-closed: anything not
// explicitly SUPPORTED is treated as unusable.
type Support string

const (
	Supported   Support = "SUPPORTED"
	Unsupported Support = "UNSUPPORTED"
	Unknown     Support = "UNKNOWN" // e.g. network isolation not yet proven
)

// Capability enumerates what a virtual Keenetic may offer. Values mirror the
// task #12 stage C inventory; a device that cannot answer reports UNKNOWN.
type Capability string

const (
	CapKeeneticOSVersion Capability = "KEENETIC_OS_VERSION"
	CapCPUArch           Capability = "CPU_ARCH" // arm64 or mipsle
	CapMemAvailable      Capability = "MEM_AVAILABLE"
	CapOptPresent        Capability = "OPT_PRESENT" // /opt exists
	CapOPKGEntware       Capability = "OPKG_ENTWARE"
	CapNetInterfaces     Capability = "NET_INTERFACES"
	CapLoopback          Capability = "LOOPBACK"
	CapExecTestBinary    Capability = "EXEC_TEST_BINARY"
	CapPIDControl        Capability = "PID_CONTROL"
	CapIsolatedTCP       Capability = "ISOLATED_TCP"
	CapIsolatedUDP       Capability = "ISOLATED_UDP"
	CapTestDNS           Capability = "TEST_DNS"
)

// AllCapabilities is the full inventory, for CheckCapabilities reports.
var AllCapabilities = []Capability{
	CapKeeneticOSVersion, CapCPUArch, CapMemAvailable, CapOptPresent,
	CapOPKGEntware, CapNetInterfaces, CapLoopback, CapExecTestBinary,
	CapPIDControl, CapIsolatedTCP, CapIsolatedUDP, CapTestDNS,
}

// ResultSource labels WHERE a result was obtained. Harness reports must
// carry this so QEMU/fake results are never mistaken for device results.
type ResultSource string

const (
	SourceLocalFake        ResultSource = "LOCAL_FAKE"
	SourceQEMUUser         ResultSource = "QEMU_USER"
	SourceKeeneticEmulator ResultSource = "KEENETIC_EMULATOR"
	SourcePhysical         ResultSource = "PHYSICAL"
)

// DeviceInfo is the static identification returned by DetectEnvironment.
type DeviceInfo struct {
	Model        string `json:"model"`
	KeeneticOS   string `json:"keenetic_os"`
	CPUArch      string `json:"cpu_arch"` // arm64 | mipsle
	EmulatorKind string `json:"emulator_kind"`
	ContractAPI  string `json:"contract_api"`
}

// Resources is the live device admission snapshot.
type Resources struct {
	MemTotalMB      int `json:"mem_total_mb"`
	MemAvailableMB  int `json:"mem_available_mb"`
	DiskAvailableMB int `json:"disk_available_mb"`
}

// TestRequest asks for one isolated execution inside the device.
type TestRequest struct {
	Name           string        `json:"name"`
	StagedArtifact string        `json:"staged_artifact"` // path inside the device
	Args           []string      `json:"args"`            // paths/names only, never secrets
	Timeout        time.Duration `json:"timeout_seconds"`
	// IsolatedNetwork requires the device-side network to be the emulated
	// one; implementations MUST refuse when isolation is not proven.
	IsolatedNetwork bool `json:"isolated_network"`
}

// TestResult reports one isolated run. ExitCode semantics belong to the
// binary (WDTT's 75 stays untouched); Err!=nil means the RUN itself failed.
type TestResult struct {
	Name      string        `json:"name"`
	ExitCode  int           `json:"exit_code"`
	Err       string        `json:"err,omitempty"`
	LogTail   []string      `json:"log_tail,omitempty"` // scrubbed, bounded
	Duration  time.Duration `json:"duration_ns"`
	StartedAt time.Time     `json:"started_at"`
}

// Environment is the `keenetic-emulator/v0` contract.
type Environment interface {
	// DetectEnvironment identifies the virtual device. Unsupported envs
	// return ErrUnsupported and a zero DeviceInfo.
	DetectEnvironment(ctx context.Context) (DeviceInfo, error)
	// CheckCapabilities answers every inventory capability; missing answers
	// default to UNKNOWN.
	CheckCapabilities(ctx context.Context) (map[Capability]Support, error)
	// ReadResources reports the live memory/disk admission figures.
	ReadResources(ctx context.Context) (Resources, error)
	// StageArtifact copies a local binary into the device and returns its
	// in-device path. Implementations must verify the copy (size+hash).
	StageArtifact(ctx context.Context, localPath string) (string, error)
	// RunIsolatedTest executes a staged binary INSIDE the device under a
	// hard timeout, in the isolated network. It must refuse (ErrUnsupported)
	// when isolation is not proven.
	RunIsolatedTest(ctx context.Context, req TestRequest) (TestResult, error)
	// CollectDiagnostics returns a secret-free diagnostics bundle.
	CollectDiagnostics(ctx context.Context) ([]byte, error)
	// Cleanup terminates every child process and removes staged artifacts.
	// It must be idempotent and safe after failures.
	Cleanup(ctx context.Context) error
}

// Compile-time proof the stub satisfies the contract.
var _ Environment = (*UnsupportedEnvironment)(nil)
