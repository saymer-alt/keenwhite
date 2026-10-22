// Package wdtt implements the KeenWhite Provider Contract v1 adapter for the
// WDTT Suite transport (GPL-3.0, run as a SEPARATE process — never linked).
//
// Stage-0 safety envelope (task #10 stage C/D):
//   - Start works only through the configured Runner: a mock (tests) or an
//     explicitly configured lab executor. The "real" execution kind is
//     refused with provider.ErrForbiddenByPolicy until hardware acceptance.
//   - Stop signals ONLY the process object this adapter started; there is no
//     API to stop by arbitrary PID.
//   - The WDTT 30-minute lab hard stop is preserved: the plan's hard-stop
//     deadline is mandatory, enforced by the adapter, and never extended.
//   - Profile contents (tokens, TURN credentials, links) are never logged,
//     never embedded in plans/diagnostics, and never placed in argv/env.
package wdtt

import (
	"context"
	"os"
	"os/exec"

	"github.com/saymer-alt/keenwhite/internal/provider"
)

// Process is the runner-neutral handle to a started transport process.
// Implementations must return the SAME values from PID/ExePath/StartTime for
// the lifetime of the process — Stop() uses them as the ownership proof.
type Process interface {
	PID() int
	ExePath() string
	StartTime() int64 // unix seconds, recorded at Start
	// Signal sends a signal (SIGTERM on Unix; Kill as last resort).
	Signal(os.Signal) error
	// Done closes when the process exits; the first read wins.
	Done() <-chan struct{}
	// ExitErr is the wait error (nil on clean exit); valid after Done.
	ExitErr() error
}

// Runner starts a process for a prepared plan. Implementations must refuse
// anything outside their safety envelope.
type Runner interface {
	Kind() provider.ExecutionKind
	Start(ctx context.Context, plan provider.StartPlan) (Process, error)
}

// --- exec runner (lab executor) -------------------------------------------

// ExecRunner starts real OS processes. It is the "lab" executor: allowed
// only in owner-lab contexts, never on production routers in this stage.
// A production executor (ExecutionReal) must be a separate, owner-approved
// implementation; this package deliberately does not provide one.
type ExecRunner struct{}

// NewExecRunner returns the lab executor.
func NewExecRunner() *ExecRunner { return &ExecRunner{} }

// Kind reports ExecutionLab.
func (r *ExecRunner) Kind() provider.ExecutionKind { return provider.ExecutionLab }

// LabProcess wraps exec.Cmd as a Process.
type LabProcess struct {
	cmd       *exec.Cmd
	startedAt int64
	done      chan struct{}
	exitErr   error
}

// Start implements Runner. argv = plan.Command + plan.Args (no secrets: the
// plan carries paths only; credentials stay inside the profile file).
func (r *ExecRunner) Start(_ context.Context, plan provider.StartPlan) (Process, error) {
	if plan.Executor == provider.ExecutionReal {
		return nil, provider.ErrForbiddenByPolicy
	}
	cmd := exec.Command(plan.Command, plan.Args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &LabProcess{cmd: cmd, startedAt: nowUnix(), done: make(chan struct{})}
	go func() {
		p.exitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// PID returns the OS pid.
func (p *LabProcess) PID() int { return p.cmd.Process.Pid }

// ExePath returns the launched binary path.
func (p *LabProcess) ExePath() string { return p.cmd.Path }

// StartTime returns the recorded start time.
func (p *LabProcess) StartTime() int64 { return p.startedAt }

// Signal forwards to the OS process.
func (p *LabProcess) Signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }

// Done closes on exit.
func (p *LabProcess) Done() <-chan struct{} { return p.done }

// ExitErr returns the wait error.
func (p *LabProcess) ExitErr() error { return p.exitErr }
