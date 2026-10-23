package acceptance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Deterministic reproduction of issue #40 (BEFORE the fix): ReadRecord must
// refuse a record whose provider or SHA directory has been swapped for a
// symlink pointing OUTSIDE the trusted root, even when the record file
// itself is intact and internally consistent. Lstat of the final component
// traverses symlinked ancestors, so the pre-fix implementation happily
// returned the externally hosted record.
//
// Each repro uses a FRESH store and moves the real directory outside the
// root, then plants a symlink at the original location — exactly the
// deterministic sequence mandated by issue #40.

func reproSwapDir(t *testing.T, swapProviderDir bool) (*Store, Record) {
	t.Helper()
	s := newStore(t)
	r := rec(nil)
	if p := mustWrite(t, s, r); p == "" {
		t.Fatal("setup write failed")
	}
	sha := shaA
	var movedFrom, movedTo string
	if swapProviderDir {
		movedFrom = filepath.Join(s.root, "wdtt")
		movedTo = filepath.Join(t.TempDir(), "evicted-provider")
	} else {
		movedFrom = filepath.Join(s.root, "wdtt", sha)
		movedTo = filepath.Join(t.TempDir(), "evicted-sha")
	}
	if err := os.Rename(movedFrom, movedTo); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(movedTo, movedFrom); err != nil {
		t.Fatal(err)
	}
	return s, r
}

// Repro 1: provider directory swapped for an external symlink.
func TestReproIssue40_ReadThroughProviderSymlink(t *testing.T) {
	canSymlink(t)
	s, r := reproSwapDir(t, true)
	got, err := s.ReadRecord("wdtt", shaA, recordIDOf(r), now)
	t.Logf("PRE-FIX RESULT: err=%v record=%+v", err, got)
	if err == nil {
		t.Fatalf("DEFEKT CONFIRMED: valid record read through symlinked provider dir: %+v", got)
	}
}

// Repro 2: SHA directory swapped for an external symlink.
func TestReproIssue40_ReadThroughShaSymlink(t *testing.T) {
	canSymlink(t)
	s, r := reproSwapDir(t, false)
	got, err := s.ReadRecord("wdtt", shaA, recordIDOf(r), now)
	t.Logf("PRE-FIX RESULT: err=%v record=%+v", err, got)
	if err == nil {
		t.Fatalf("DEFEKT CONFIRMED: valid record read through symlinked sha dir: %+v", got)
	}
}

// helperSharedRepro mirrors the issue #40 reproduction but asserts the
// POST-FIX contract; used as the permanent regression suite.
func helperSharedRepro(t *testing.T, swapProviderDir bool) {
	t.Helper()
	canSymlink(t)
	s, r := reproSwapDir(t, swapProviderDir)
	if _, err := s.ReadRecord("wdtt", shaA, recordIDOf(r), now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("read through swapped %s dir must refuse with ErrUnsafePath, got %v",
			map[bool]string{true: "provider", false: "sha"}[swapProviderDir], err)
	}
	// Listing must surface the hazard too — not silently accept or omit.
	_, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) == 0 {
		t.Fatal("listing must report a record reachable only through a swapped intermediate dir")
	}
	for _, p := range probs {
		if !strings.Contains(p.Reason, "unsafe") && !strings.Contains(p.Reason, "symlink") {
			t.Fatalf("problem must name the integrity reason, got %+v", p)
		}
	}
}

// POST-FIX regression: both swap variants must fail closed on read and be
// reported by listing.
func TestReadPath_ProviderDirSymlinkRefused(t *testing.T) {
	helperSharedRepro(t, true)
}

func TestReadPath_ShaDirSymlinkRefused(t *testing.T) {
	helperSharedRepro(t, false)
}

// A symlinked selector root in ListRecords must be refused as well.
func TestReadPath_ListSelectorSymlinkRefused(t *testing.T) {
	canSymlink(t)
	s, r := reproSwapDir(t, true) // provider dir is a symlink to outside
	if _, _, err := s.ListRecords("wdtt", "", now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("listing with a symlinked provider selector must refuse, got %v", err)
	}
	_ = r
}

// The swapped-dir scenario must also block a WRITE through the same path
// (already covered by stage C of task 15, re-asserted here together with
// the read path for the exact issue-#40 sequence).
func TestReadPath_WriteThroughSwappedDirStillRefused(t *testing.T) {
	canSymlink(t)
	s, r := reproSwapDir(t, false)
	if _, err := s.WriteRecord(r, now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("write through swapped sha dir must refuse, got %v", err)
	}
}

var _ = time.Now

// Task 16 stage C: ListRecords must SURFACE every malformed element, never
// silently ignore it. Directory instead of record file:
func TestList_DirectoryInsteadOfRecordReported(t *testing.T) {
	s := newStore(t)
	dir := filepath.Join(s.root, "wdtt", shaA)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tcp_e2e-20200101t000000-bogus.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 || len(probs) != 1 {
		t.Fatalf("directory-as-record must be reported: %d entries / %d problems", len(ents), len(probs))
	}
}

// File instead of directory at the provider level (full walk AND selector).
func TestList_FileInsteadOfDirectoryReported(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "wdtt"), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 || len(probs) != 1 {
		t.Fatalf("file-as-provider-dir must be reported: %d entries / %d problems", len(ents), len(probs))
	}
	if _, _, err := s.ListRecords("wdtt", "", now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("selector through a file must refuse, got %v", err)
	}
}

// Missing provider directory in a selector is refused, not a silent empty
// result.
func TestList_MissingSelectorDirectoryRefused(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.ListRecords("nosuch", "", now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("missing provider selector must refuse, got %v", err)
	}
	// Empty full walk on a fresh store stays a legitimate empty result.
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil || len(ents) != 0 || len(probs) != 0 {
		t.Fatalf("empty store walk must be clean: %v %d %d", err, len(ents), len(probs))
	}
}

// A foreign file at record depth is reported, while our own .tmp-* writers'
// housekeeping files remain ignored.
func TestList_ForeignFileReportedTmpIgnored(t *testing.T) {
	s := newStore(t)
	mustWrite(t, s, rec(nil))
	dir := filepath.Join(s.root, "wdtt", shaA)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-crashed-writer"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Fatalf("valid record must be listed: %d", len(ents))
	}
	if len(probs) != 1 || !strings.Contains(probs[0].Reason, "unexpected layout") {
		t.Fatalf("foreign file must be reported: %+v", probs)
	}
	for _, p := range probs {
		if strings.Contains(p.Path, ".tmp-") {
			t.Fatal("our own temp files must stay ignored")
		}
	}
}
