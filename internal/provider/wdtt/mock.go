package wdtt

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/saymer-alt/keenwhite/internal/provider"
)

// MockProcess is an in-memory Process for tests and the harness. It can be
// scripted to crash, hang, or ignore signals.
type MockProcess struct {
	mu         sync.Mutex
	pid        int
	exe        string
	started    int64
	done       chan struct{}
	exitErr    error
	ignoresSig bool // scenario: hung process that survives SIGTERM
	sigTerm    int
	sigKill    int
}

// NewMockProcess builds a scripted process handle.
func NewMockProcess(pid int, exe string, started int64) *MockProcess {
	return &MockProcess{pid: pid, exe: exe, started: started, done: make(chan struct{})}
}

// PID returns the scripted pid.
func (p *MockProcess) PID() int { return p.pid }

// ExePath returns the scripted exe path.
func (p *MockProcess) ExePath() string { return p.exe }

// StartTime returns the scripted start time.
func (p *MockProcess) StartTime() int64 { return p.started }

// Signal counts signals; a hanging process ignores SIGTERM but not SIGKILL.
func (p *MockProcess) Signal(sig os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sig == os.Kill {
		p.sigKill++
		p.exitErr = errors.New("killed")
		p.closeOnce()
		return nil
	}
	p.sigTerm++
	if p.ignoresSig {
		return nil
	}
	p.exitErr = nil
	p.closeOnce()
	return nil
}

// Done closes when the process exits.
func (p *MockProcess) Done() <-chan struct{} { return p.done }

// ExitErr returns the exit error.
func (p *MockProcess) ExitErr() error { return p.exitErr }

// SetIgnoreSignals scripts the process to ignore SIGTERM (hung scenario).
func (p *MockProcess) SetIgnoreSignals() {
	p.mu.Lock()
	p.ignoresSig = true
	p.mu.Unlock()
}

// Crash simulates a non-zero exit (scenario: process failure).
func (p *MockProcess) Crash() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exitErr = errors.New("exit status 1")
	p.closeOnce()
}

// ExitsCleanly simulates a normal exit.
func (p *MockProcess) ExitsCleanly() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeOnce()
}

// SignalCounts exposes recorded signal counts for assertions.
func (p *MockProcess) SignalCounts() (term, kill int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sigTerm, p.sigKill
}

func (p *MockProcess) closeOnce() {
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

// MockRunner starts scripted processes and records what it was asked to run.
type MockRunner struct {
	mu       sync.Mutex
	started  []*MockProcess
	nextPID  int
	failNext error
	// PlanHook lets tests inspect/modify the plan at start time.
	PlanHook func(provider.StartPlan)
}

// NewMockRunner builds an empty mock runner (kind: mock).
func NewMockRunner() *MockRunner { return &MockRunner{nextPID: 4200} }

// Kind reports ExecutionMock.
func (r *MockRunner) Kind() provider.ExecutionKind { return provider.ExecutionMock }

// FailNext makes the next Start return err (scenario: runner failure).
func (r *MockRunner) FailNext(err error) { r.failNext = err }

// StartedCount reports how many processes were started.
func (r *MockRunner) StartedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started)
}

// Last returns the last started process (or nil).
func (r *MockRunner) Last() *MockProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.started) == 0 {
		return nil
	}
	return r.started[len(r.started)-1]
}

// Start implements Runner.
func (r *MockRunner) Start(_ context.Context, plan provider.StartPlan) (Process, error) {
	r.mu.Lock()
	if r.failNext != nil {
		err := r.failNext
		r.failNext = nil
		r.mu.Unlock()
		return nil, err
	}
	r.nextPID++
	p := NewMockProcess(r.nextPID, plan.Command, time.Now().Unix())
	r.started = append(r.started, p)
	hook := r.PlanHook
	r.mu.Unlock()
	if hook != nil {
		hook(plan)
	}
	return p, nil
}
