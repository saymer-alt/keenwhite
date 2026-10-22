package wdtt

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
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

// LabGrant is the explicit owner-side authorisation for the lab executor.
// A bare ExecutionLab kind is NOT a permission (task #11 B2): the grant
// names the ONE binary that may be launched, pinned by its SHA256. There is
// deliberately no constructor that yields an unscoped ExecRunner, and no
// way to launch an arbitrary command from an unconfirmed manifest.
type LabGrant struct {
	// BinaryPath is the only executable this grant authorises.
	BinaryPath string
	// SHA256 pins the binary contents (64 lowercase hex). Verified at every
	// Start with a constant-time compare over a fresh hash of the file.
	SHA256 string
}

// ErrGrantDenied is returned when a launch is not covered by a valid grant.
var ErrGrantDenied = errors.New("lab grant denied: no grant, wrong binary path, or binary hash mismatch")

// --- exec runner (grant-gated lab executor) --------------------------------

// ExecRunner starts real OS processes ONLY within a LabGrant. Without a
// valid grant every Start returns ErrGrantDenied — the runner cannot launch
// anything, so an unconfirmed manifest or a swapped plan cannot reach the OS.
type ExecRunner struct {
	grant LabGrant
}

// NewExecRunner returns a grant-gated lab executor. A zero grant denies
// everything; production ("real router") execution must be a separate,
// owner-approved implementation that this package intentionally does not
// provide.
func NewExecRunner(grant LabGrant) *ExecRunner { return &ExecRunner{grant: grant} }

// Kind reports ExecutionLab.
func (r *ExecRunner) Kind() provider.ExecutionKind { return provider.ExecutionLab }

// Grant verifies the plan against the grant WITHOUT launching anything:
// the command path must equal the granted path, and the on-disk file must
// currently hash to the granted SHA256 (constant-time compare).
func (r *ExecRunner) Grant(plan provider.StartPlan) error {
	if r.grant.BinaryPath == "" || len(r.grant.SHA256) != 64 {
		return ErrGrantDenied
	}
	if plan.Command != r.grant.BinaryPath {
		return ErrGrantDenied
	}
	f, err := os.Open(plan.Command)
	if err != nil {
		return fmtHashErr(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmtHashErr(err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(got), []byte(r.grant.SHA256)) != 1 {
		return ErrGrantDenied
	}
	return nil
}

func fmtHashErr(err error) error { return errors.Join(ErrGrantDenied, err) }

// LabProcess wraps exec.Cmd as a Process.
type LabProcess struct {
	cmd       *exec.Cmd
	startedAt int64
	done      chan struct{}
	exitErr   error
}

// Start implements Runner. argv = plan.Command + plan.Args (no secrets: the
// plan carries paths only; credentials stay inside the profile file). The
// grant is re-verified immediately before every launch.
func (r *ExecRunner) Start(ctx context.Context, plan provider.StartPlan) (Process, error) {
	if plan.Executor == provider.ExecutionReal {
		return nil, provider.ErrForbiddenByPolicy
	}
	if err := r.Grant(plan); err != nil {
		return nil, err
	}
	cmd := exec.Command(plan.Command, plan.Args...)
	if ctx != nil {
		cmd = exec.CommandContext(ctx, plan.Command, plan.Args...)
	}
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
