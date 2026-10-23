package emulator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MockEnvironment is an honest in-process test double for the
// `keenetic-emulator/v0` contract (task 14 stage D4). It is TEST
// INFRASTRUCTURE, not an emulator:
//
//   - it never fabricates successful results: every RunIsolatedTest refuses
//     (a mock that "runs" binaries would be exactly the fabricated-success
//     failure mode the stage forbids);
//   - capabilities default to UNKNOWN — an explicit IsolatedNetwork answer
//     must be configured by the TEST, never invented;
//   - staging is real (local temp dir, size+SHA256 verification) so callers
//     exercise the honest staging path;
//   - cleanup removes everything staged and is idempotent.
//
// The future QemuEnvironment replaces this type; nothing in KeenWhite's
// production paths may depend on it.
type MockEnvironment struct {
	mu          sync.Mutex
	dir         string
	staged      map[string]string // in-mock path -> sha256
	cleanedUp   bool
	capOverride map[Capability]Support
}

// NewMockEnvironment creates a mock backed by a fresh temp directory.
func NewMockEnvironment() (*MockEnvironment, error) {
	dir, err := os.MkdirTemp("", "keenwhite-mock-emulator-*")
	if err != nil {
		return nil, fmt.Errorf("create mock root: %w", err)
	}
	return &MockEnvironment{dir: dir, staged: map[string]string{}, capOverride: map[Capability]Support{}}, nil
}

// SetCapability lets a TEST pre-answer one capability honestly; production
// code must never call this.
func (m *MockEnvironment) SetCapability(c Capability, s Support) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capOverride[c] = s
}

// Root exposes the staging directory for test assertions.
func (m *MockEnvironment) Root() string { return m.dir }

func (m *MockEnvironment) DetectEnvironment(ctx context.Context) (DeviceInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cleanedUp {
		return DeviceInfo{}, errors.New("mock environment already cleaned up")
	}
	return DeviceInfo{Model: "mock-keenetic", KeeneticOS: "unknown",
		CPUArch: "unknown", EmulatorKind: "in-process-mock", ContractAPI: APIVersionV0}, nil
}

func (m *MockEnvironment) CheckCapabilities(ctx context.Context) (map[Capability]Support, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[Capability]Support, len(AllCapabilities))
	for _, c := range AllCapabilities {
		if s, ok := m.capOverride[c]; ok {
			out[c] = s
		} else {
			out[c] = Unknown // honest default: the mock does not know
		}
	}
	return out, nil
}

func (m *MockEnvironment) ReadResources(ctx context.Context) (Resources, error) {
	return Resources{}, ErrUnsupported // the mock reports no admission figures
}

// StageArtifact copies a local file into the mock staging dir, verifying
// size and SHA256 like a real environment must.
func (m *MockEnvironment) StageArtifact(ctx context.Context, localPath string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cleanedUp {
		return "", errors.New("mock environment already cleaned up")
	}
	b, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("stage: read local artifact: %w", err)
	}
	sum := sha256.Sum256(b)
	name := filepath.Base(localPath)
	if name == "" || name == "." || name == string(filepath.Separator) || strings.Contains(name, "..") {
		return "", errors.New("stage: invalid artifact name")
	}
	inPath := filepath.Join(m.dir, "staged-"+name)
	if err := os.WriteFile(inPath, b, 0o600); err != nil {
		return "", fmt.Errorf("stage: write staged artifact: %w", err)
	}
	// Verify the copy like the contract requires (size+hash).
	check, err := os.ReadFile(inPath)
	if err != nil || len(check) != len(b) {
		return "", errors.New("stage: staged copy size mismatch")
	}
	if got := sha256.Sum256(check); hex.EncodeToString(got[:]) != hex.EncodeToString(sum[:]) {
		return "", errors.New("stage: staged copy hash mismatch")
	}
	m.staged[inPath] = hex.EncodeToString(sum[:])
	return inPath, nil
}

// RunIsolatedTest ALWAYS refuses. A mock cannot prove network isolation and
// must not simulate execution — refusing is the honest answer.
func (m *MockEnvironment) RunIsolatedTest(ctx context.Context, req TestRequest) (TestResult, error) {
	return TestResult{}, ErrUnsupported
}

func (m *MockEnvironment) CollectDiagnostics(ctx context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cleanedUp {
		return nil, errors.New("mock environment already cleaned up")
	}
	return []byte(fmt.Sprintf(`{"mock":true,"staged":%d,"secrets":"none"}`, len(m.staged))), nil
}

// Cleanup removes every staged artifact and marks the mock finished. It is
// idempotent and safe after failures.
func (m *MockEnvironment) Cleanup(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cleanedUp {
		return nil
	}
	if err := os.RemoveAll(m.dir); err != nil {
		return fmt.Errorf("cleanup mock root: %w", err)
	}
	m.cleanedUp = true
	m.staged = map[string]string{}
	return nil
}
