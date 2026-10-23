package acceptance

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Task 16 stage D2: append-only proven across INDEPENDENT OS PROCESSES.
// The helper mode is selected by an environment variable inside TestMain,
// so the child process runs no tests — it just opens the same store root
// and attempts the same deterministic record. Exactly one process may win.

const (
	helperModeEnv  = "KW_ACCEPTANCE_HELPER_MODE"
	helperRootEnv  = "KW_ACCEPTANCE_HELPER_ROOT"
	helperNotesEnv = "KW_ACCEPTANCE_HELPER_NOTES"
)

func TestMain(m *testing.M) {
	switch os.Getenv(helperModeEnv) {
	case "write-record":
		os.Exit(runInterprocessWriteHelper())
	}
	os.Exit(m.Run())
}

// runInterprocessWriteHelper attempts to publish the mandated record into
// the store root passed via the environment. Exit codes: 0 = published
// (winner), 3 = refused with ErrRecordExists (honest loser), 2/1 = setup
// or unexpected failure (test fails, never silently skipped).
func runInterprocessWriteHelper() int {
	root := os.Getenv(helperRootEnv)
	notes := os.Getenv(helperNotesEnv)
	s, err := OpenStore(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper: open store: %v\n", err)
		return 2
	}
	r := rec(func(x *Record) { x.Notes = notes })
	if _, err := s.WriteRecord(r, now); err != nil {
		if errors.Is(err, ErrRecordExists) {
			fmt.Fprintf(os.Stderr, "helper: lost publication race (ErrRecordExists)\n")
			return 3
		}
		fmt.Fprintf(os.Stderr, "helper: write: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "helper: published\n")
	return 0
}

// TestInterprocess_SameRecordIDExactlyOneWinner starts TWO independent OS
// processes that race for the same record id. Exactly one exits 0, the
// other must exit 3 (ErrRecordExists) — the decision is the kernel's
// atomic link(), not any preflight check. The winner's content is intact
// and the listing is clean. If the test runner cannot exec itself, the
// test is SKIPPED with the reason — never a false PASS.
func TestInterprocess_SameRecordIDExactlyOneWinner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot resolve the test binary: %v", err)
	}
	baseEnv := append(os.Environ(),
		helperModeEnv+"=write-record",
		helperRootEnv+"="+root,
	)
	run := func(notes string) *exec.Cmd {
		cmd := exec.Command(exe)
		cmd.Env = append(baseEnv, helperNotesEnv+"="+notes)
		return cmd
	}
	cmds := []*exec.Cmd{run("interprocess-writer-A"), run("interprocess-writer-B")}

	var wg sync.WaitGroup
	startErrs := make([]error, 2)
	states := make([]*os.ProcessState, 2)
	for i, cmd := range cmds {
		i, cmd := i, cmd
		wg.Add(1)
		go func() {
			defer wg.Done()
			startErrs[i] = cmd.Start()
			if startErrs[i] == nil {
				states[i], _ = cmd.Process.Wait()
			}
		}()
	}
	wg.Wait()

	started := 0
	for i, err := range startErrs {
		if err != nil {
			// Only report exec failures; the sibling decides skip vs fail.
			t.Logf("process %d did not start: %v", i, err)
		} else {
			started++
		}
	}
	if started == 0 {
		t.Skipf("the environment cannot exec the test binary; interprocess race not exercised")
	}
	if started < 2 {
		t.Fatalf("both processes must start for a meaningful race; started=%d", started)
	}

	winners, losers := 0, 0
	for i, st := range states {
		if st == nil {
			t.Fatalf("process %d produced no exit state", i)
		}
		switch code := st.ExitCode(); code {
		case 0:
			winners++
		case 3:
			losers++
		default:
			t.Fatalf("process %d: unexpected exit code %d (must be 0 winner or 3 ErrRecordExists loser)", i, code)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("exactly one winner and one loser expected, got winners=%d losers=%d", winners, losers)
	}

	// The winner's record is intact and the store lists exactly one entry.
	s, err := OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadRecord("wdtt", shaA, recordIDOf(rec(nil)), now)
	if err != nil {
		t.Fatalf("winner record must be readable: %v", err)
	}
	if !strings.HasPrefix(got.Notes, "interprocess-writer-") {
		t.Fatalf("winner content must be one of the racing payloads, got %q", got.Notes)
	}
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || len(probs) != 0 {
		t.Fatalf("store must contain exactly one clean record: %d entries / %d problems", len(ents), len(probs))
	}
}
