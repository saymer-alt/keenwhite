// Package provider defines KeenWhite's versioned provider contract (v1):
// the interface every transport adapter implements, plus the evidence,
// lifecycle-state and freshness vocabulary shared by adapters and harnesses.
//
// Safety invariants, in force for every implementation:
//   - adapters run transports as SEPARATE processes; KeenWhite never links
//     third-party transport code into its own binary;
//   - loopback SOCKS binds only (127.0.0.1), and two providers must never
//     share a bind (the catalog enforces this statically);
//   - evidence levels describe how a claim is BACKED, never that a tunnel
//     currently works; TRAFFIC_E2E_VERIFIED requires a fresh measurement;
//   - a provider's hard time limit (e.g. the WDTT 30-minute lab stop) is
//     owned by the adapter and may never be extended or dropped;
//   - no secrets in argv, logs, or diagnostics: config values stay in files,
//     and every structure here is safe to marshal.
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// APIVersionV1 is the contract version implemented by this package.
const APIVersionV1 = "keenwhite.provider/v1"

// Evidence grades how strongly a capability claim is backed.
// Reused from the MT-001 vocabulary (KAS PR #175) with identical semantics.
type Evidence string

const (
	EvidenceUnknown         Evidence = "UNKNOWN"
	EvidenceUnsupported     Evidence = "UNSUPPORTED"
	EvidenceSourceConfirmed Evidence = "SOURCE_CONFIRMED"
	EvidenceBuildConfirmed  Evidence = "BUILD_CONFIRMED"
	EvidenceE2EVerified     Evidence = "E2E_VERIFIED"
)

// Valid reports whether e is one of the five contract evidence levels.
func (e Evidence) Valid() bool {
	switch e {
	case EvidenceUnknown, EvidenceUnsupported, EvidenceSourceConfirmed,
		EvidenceBuildConfirmed, EvidenceE2EVerified:
		return true
	}
	return false
}

// LifecycleState names the separate runtime concepts a manager tracks.
// PROCESS_RUNNING, LISTENER_READY and TRAFFIC_E2E_VERIFIED are deliberately
// distinct: a running process is not a listening listener, and a loopback
// listener alone is never proof of working transport.
type LifecycleState string

const (
	StateIdle              LifecycleState = "IDLE"
	StatePreparing         LifecycleState = "PREPARING"
	StateProcessRunning    LifecycleState = "PROCESS_RUNNING"
	StateListenerReady     LifecycleState = "LISTENER_READY"
	StateTrafficE2E        LifecycleState = "TRAFFIC_E2E_VERIFIED"
	StateStopped           LifecycleState = "STOPPED"
	StateFailed            LifecycleState = "FAILED"
	StateForbiddenByPolicy LifecycleState = "FORBIDDEN_BY_POLICY"
)

// ErrForbiddenByPolicy is returned when an operation would leave the offline
// safety envelope (e.g. a real process start before hardware acceptance).
var ErrForbiddenByPolicy = errors.New("forbidden by policy: this KeenWhite build only allows offline checks and mock/lab executors")

// Provider is the versioned adapter contract. Adapters are expected to be
// safe to construct from a parsed manifest alone and must not perform any
// work before Validate/Prepare are called.
type Provider interface {
	// Name is the provider identifier (catalog ID, e.g. "wdtt").
	Name() string
	// APIVersion reports the contract version the adapter implements.
	APIVersion() string
	// Validate checks a configuration document statically. It never touches
	// the filesystem beyond reading the referenced files and never executes
	// anything.
	Validate(ctx context.Context) []Problem
	// Prepare verifies the artifact (hash+arch), required profile files,
	// available resources and the SOCKS bind, and produces an executable
	// StartPlan without executing it.
	Prepare(ctx context.Context) (PrepareResult, error)
	// Status reports the current lifecycle state snapshot.
	Status(ctx context.Context) (Status, error)
	// Start performs a controlled launch through the configured Runner.
	// Until hardware acceptance, adapters must be wired to a mock/lab Runner;
	// a real executor must refuse by returning ErrForbiddenByPolicy.
	Start(ctx context.Context) error
	// Stop terminates ONLY the process this adapter started, verifying its
	// recorded identity first. It must never signal an arbitrary PID.
	Stop(ctx context.Context) error
	// Health returns a fresh diagnostics snapshot with its measurement time.
	Health(ctx context.Context) (Health, error)
}

// Problem is one validation finding. Severity is "error" or "warning".
type Problem struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func Errorf(field, format string, args ...any) Problem {
	return Problem{Severity: "error", Field: field, Message: sprintf(format, args...)}
}

func Warnf(field, format string, args ...any) Problem {
	return Problem{Severity: "warning", Field: field, Message: sprintf(format, args...)}
}

// StartPlan is the fully materialised launch specification. It is safe to
// marshal: environment is represented by NAMES only, and profile contents
// are referenced by path, never embedded.
type StartPlan struct {
	Command         string        `json:"command"`
	Args            []string      `json:"args"`
	EnvKeys         []string      `json:"env_keys"`
	ConfigPaths     []string      `json:"config_paths"`
	SocksBind       string        `json:"socks_bind"`
	HardStopSeconds int           `json:"hard_stop_seconds"`
	Deadline        time.Time     `json:"deadline"`
	Executor        ExecutionKind `json:"executor"`
}

// ExecutionKind declares which runner may execute a StartPlan.
type ExecutionKind string

const (
	ExecutionMock ExecutionKind = "mock" // in-process fake, for tests only
	ExecutionLab  ExecutionKind = "lab"  // explicitly configured lab executor
	ExecutionReal ExecutionKind = "real" // REAL ROUTER EXECUTION — refused in this stage
)

// PrepareResult is the outcome of Prepare.
type PrepareResult struct {
	Provider  string          `json:"provider"`
	Manifest  ManifestSummary `json:"manifest"`
	Artifacts []ArtifactClaim `json:"artifacts"`
	Resources Resources       `json:"resources"`
	Plan      StartPlan       `json:"start_plan"`
	Problems  []Problem       `json:"problems,omitempty"`
}

// ManifestSummary is the redacted manifest digest used in reports.
type ManifestSummary struct {
	Version  int    `json:"manifest_version"`
	Provider string `json:"provider"`
	Bind     string `json:"bind"`
}

// ArtifactClaim records one verified binary (reuses internal/artifact).
type ArtifactClaim struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	Architecture string `json:"architecture"`
	Verification string `json:"verification"`
}

// Resources is the device admission snapshot (injected, testable).
type Resources struct {
	MemAvailableMB int    `json:"mem_available_mb"`
	Admitted       bool   `json:"admitted"`
	Reason         string `json:"reason,omitempty"`
}

// Status is the lifecycle snapshot returned by Status().
type Status struct {
	State      LifecycleState `json:"state"`
	Process    *ProcessInfo   `json:"process,omitempty"`
	Listener   ListenerStatus `json:"listener"`
	TCPE2E     Evidence       `json:"tcp_e2e"`
	UDPE2E     Evidence       `json:"udp_e2e"`
	CheckedAt  time.Time      `json:"checked_at"`
	DeadlineAt *time.Time     `json:"deadline_at,omitempty"`
}

// ProcessInfo carries the recorded identity of OUR process. The identity is
// what Stop() checks before signalling anything.
type ProcessInfo struct {
	PID       int    `json:"pid"`
	ExePath   string `json:"exe_path"`
	StartTime int64  `json:"start_time_unix"`
}

// ListenerStatus describes the loopback SOCKS listener claim.
type ListenerStatus struct {
	Ready  bool   `json:"ready"`
	Bind   string `json:"bind"`
	Detail string `json:"detail,omitempty"`
}

// TrafficStatus keeps E2E traffic evidence separate from listener readiness.
type TrafficStatus struct {
	TCPE2E      Evidence  `json:"tcp_e2e"`
	UDPE2E      Evidence  `json:"udp_e2e"`
	MeasuredAt  time.Time `json:"measured_at,omitempty"`
	EvidenceURL string    `json:"evidence_url,omitempty"`
}

// Health is the diagnostics snapshot. Freshness is explicit: results older
// than TTL are stale and MUST NOT be treated as proof of anything.
type Health struct {
	State         LifecycleState    `json:"state"`
	ProcessAlive  bool              `json:"process_alive"`
	ListenerReady bool              `json:"listener_ready"`
	TCPE2E        Evidence          `json:"tcp_e2e"`
	UDPE2E        Evidence          `json:"udp_e2e"`
	MeasuredAt    time.Time         `json:"measured_at"`
	TTL           time.Duration     `json:"ttl_seconds"`
	Details       map[string]string `json:"details,omitempty"`
}

// DefaultHealthTTL is how long a Health snapshot stays fresh by default.
const DefaultHealthTTL = 30 * time.Second

// Stale reports whether this snapshot has expired at time now.
func (h Health) Stale(now time.Time) bool {
	if h.TTL <= 0 {
		h.TTL = DefaultHealthTTL
	}
	return now.Sub(h.MeasuredAt) > h.TTL
}

// EffectiveTCP returns the TCP evidence after freshness downgrading: a stale
// E2E claim is reported as UNKNOWN, never as proof.
func (h Health) EffectiveTCP(now time.Time) Evidence {
	if h.Stale(now) && h.TCPE2E == EvidenceE2EVerified {
		return EvidenceUnknown
	}
	return h.TCPE2E
}

// EffectiveUDP is EffectiveTCP for the UDP axis.
func (h Health) EffectiveUDP(now time.Time) Evidence {
	if h.Stale(now) && h.UDPE2E == EvidenceE2EVerified {
		return EvidenceUnknown
	}
	return h.UDPE2E
}

func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
