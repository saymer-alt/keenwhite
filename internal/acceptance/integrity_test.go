package acceptance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Task 15 / issue #38: mandated integrity and stress scenarios for the
// acceptance evidence store. All run on the real filesystem; symlink
// scenarios are skipped where the platform cannot create them.

var shaA = strings.Repeat("a", 64)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustWrite(t *testing.T, s *Store, r Record) string {
	t.Helper()
	p, err := s.WriteRecord(r, now)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func canSymlink(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	link := filepath.Join(dir, "probe")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable on this platform/filesystem: %v", err)
	}
	os.Remove(link)
	return true
}

// Scenario 1: concurrent writes of the SAME record id — exactly one winner,
// losers get ErrRecordExists, stored content is one writer's data without
// any interleaving (independent goroutines, no local mutex involved).
func TestIntegrity_ConcurrentSameIDExactlyOneWinner(t *testing.T) {
	s := newStore(t)
	const writers = 8
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := rec(func(x *Record) { x.Notes = fmt.Sprintf("writer-%d", i) })
			_, errs[i] = s.WriteRecord(r, now)
		}(i)
	}
	wg.Wait()
	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case !errors.Is(err, ErrRecordExists):
			t.Fatalf("writer %d: unexpected error: %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("exactly one writer must win, got %d", winners)
	}
	id := recordIDOf(rec(nil))
	got, err := s.ReadRecord("wdtt", shaA, id, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Notes, "writer-") {
		t.Fatalf("winner content must be intact: %q", got.Notes)
	}
	// The stored file carries exactly ONE writer's payload — no mix.
	b, err := os.ReadFile(filepath.Join(s.root, "wdtt", shaA, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "writer-"); n != 1 {
		t.Fatalf("stored content must contain exactly one writer payload, found %d", n)
	}
	// Losers' files do not exist anywhere else (same single id path).
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || len(probs) != 0 {
		t.Fatalf("store must contain exactly 1 clean record: %d/%d", len(ents), len(probs))
	}
}

// Scenario 2: an existing record is never modified by a losing write with
// different content (same deterministic id, different payload).
func TestIntegrity_ExistingRecordNeverModified(t *testing.T) {
	s := newStore(t)
	id := recordIDOf(rec(nil))
	mustWrite(t, s, rec(nil))
	path := filepath.Join(s.root, "wdtt", shaA, id+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tamper := rec(func(x *Record) { x.Result = ResultFAIL; x.Notes = "attempted overwrite" })
	_, err = s.WriteRecord(tamper, now)
	if !errors.Is(err, ErrRecordExists) {
		t.Fatalf("overwrite must be refused with ErrRecordExists, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("existing record content changed — append-only violated")
	}
}

// Scenario 3: concurrent writes through SEPARATE Store instances on one
// root (the cross-process case; distinct records all land cleanly).
func TestIntegrity_ConcurrentDistinctInstances(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	const instances, perInstance = 4, 3
	stores := make([]*Store, instances)
	for i := range stores {
		var err error
		stores[i], err = OpenStore(root)
		if err != nil {
			t.Fatal(err)
		}
	}
	type result struct{ err error }
	results := make([]result, instances*perInstance)
	var wg sync.WaitGroup
	for i := 0; i < instances; i++ {
		for j := 0; j < perInstance; j++ {
			i, j := i, j
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := rec(func(x *Record) {
					x.ProfileLabel = fmt.Sprintf("synthetic-load-%d-%d", i, j)
					x.MeasuredAt = now.Add(-time.Duration(j+1) * time.Hour)
				})
				_, results[i*perInstance+j].err = stores[i].WriteRecord(r, now)
			}()
		}
	}
	wg.Wait()
	for k, res := range results {
		if res.err != nil {
			t.Fatalf("write %d failed: %v", k, res.err)
		}
	}
	s := stores[0]
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != instances*perInstance || len(probs) != 0 {
		t.Fatalf("expected %d clean records, got %d entries / %d problems",
			instances*perInstance, len(ents), len(probs))
	}
}

// Scenarios 4/5/6: a valid record MOVED to another provider dir, another
// sha dir or another record-id filename must fail closed with ErrIntegrity
// (ReadRecord refuses; ListRecords reports a Problem instead of accepting).
func TestIntegrity_ProviderDirMismatch(t *testing.T) {
	s := newStore(t)
	r := rec(nil)
	id := recordIDOf(r)
	mustWrite(t, s, r)
	if err := os.Rename(filepath.Join(s.root, "wdtt"), filepath.Join(s.root, "wdtx")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadRecord("wdtx", shaA, id, now); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("moved provider must fail with ErrIntegrity, got %v", err)
	}
	_, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 1 {
		t.Fatalf("listing must report the moved record as a problem: %+v", probs)
	}
}

func TestIntegrity_ShaDirMismatch(t *testing.T) {
	s := newStore(t)
	r := rec(nil)
	id := recordIDOf(r)
	mustWrite(t, s, r)
	shaB := strings.Repeat("b", 64)
	if err := os.Rename(filepath.Join(s.root, "wdtt", shaA), filepath.Join(s.root, "wdtt", shaB)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadRecord("wdtt", shaB, id, now); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("moved sha must fail with ErrIntegrity, got %v", err)
	}
}

func TestIntegrity_RecordIDMismatch(t *testing.T) {
	s := newStore(t)
	r := rec(nil)
	id := recordIDOf(r)
	mustWrite(t, s, r)
	b, err := os.ReadFile(filepath.Join(s.root, "wdtt", shaA, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	forged := "tcp_e2e-29990101t000000-forged"
	dst := filepath.Join(s.root, "wdtt", shaA, forged+".json")
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadRecord("wdtt", shaA, forged, now); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("renamed copy must fail with ErrIntegrity, got %v", err)
	}
	_, probs, _ := s.ListRecords("", "", now)
	if len(probs) != 1 {
		t.Fatalf("listing must report the forged filename: %+v", probs)
	}
}

// Scenario 7: a symlink placed at the record path — WriteRecord refuses
// (no write-through), ReadRecord refuses (not a regular file).
func TestIntegrity_SymlinkAtRecordPath(t *testing.T) {
	canSymlink(t)
	s := newStore(t)
	real := rec(func(x *Record) { x.MeasuredAt = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) })
	mustWrite(t, s, real)
	victim := rec(func(x *Record) { x.MeasuredAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) })
	victimID := recordIDOf(victim)
	target := filepath.Join(s.root, "wdtt", shaA, victimID+".json")
	realPath := filepath.Join(s.root, "wdtt", shaA, recordIDOf(real)+".json")
	if err := os.Symlink(realPath, target); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteRecord(victim, now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("write over a symlink must refuse with ErrUnsafePath, got %v", err)
	}
	if _, err := s.ReadRecord("wdtt", shaA, victimID, now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("read through a symlink must refuse with ErrUnsafePath, got %v", err)
	}
	_, probs, _ := s.ListRecords("", "", now)
	if len(probs) != 1 {
		t.Fatalf("listing must report the symlinked record: %+v", probs)
	}
}

// Scenario 8: a symlinked INTERMEDIATE directory (sha dir and provider dir)
// must never be written through or read through.
func TestIntegrity_SymlinkedIntermediateDir(t *testing.T) {
	canSymlink(t)
	s := newStore(t)
	r := rec(nil)
	mustWrite(t, s, r)
	outside := t.TempDir() // outside the trusted root

	shaDir := filepath.Join(s.root, "wdtt", shaA)
	if err := os.RemoveAll(shaDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, shaDir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteRecord(r, now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("write through symlinked sha dir must refuse, got %v", err)
	}
	// Provider dir level too (fresh store to avoid the replaced sha dir).
	s2 := newStore(t)
	if err := os.Symlink(outside, filepath.Join(s2.root, "wdtt")); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.WriteRecord(r, now); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("write through symlinked provider dir must refuse, got %v", err)
	}
}

// Scenario 9 (kept explicit for the mandate): corrupted JSON in a stored
// record must surface as a listing problem and a read error.
func TestIntegrity_CorruptedJSONReported(t *testing.T) {
	s := newStore(t)
	id := recordIDOf(rec(nil))
	mustWrite(t, s, rec(nil))
	path := filepath.Join(s.root, "wdtt", shaA, id+".json")
	if err := os.WriteFile(path, []byte("{corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadRecord("wdtt", shaA, id, now); err == nil {
		t.Fatal("corrupted record must fail to read")
	}
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 || len(probs) != 1 {
		t.Fatalf("corrupted entry must surface: %d entries, %d problems", len(ents), len(probs))
	}
}

// Scenario 10: an interrupted write (crashed writer left a private temp
// file) is invisible to listing and does not block a retry.
func TestIntegrity_InterruptedWriteLeftoverTemp(t *testing.T) {
	s := newStore(t)
	dir := filepath.Join(s.root, "wdtt", shaA)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-orphan"), []byte("{partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	ents, probs, err := s.ListRecords("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 || len(probs) != 0 {
		t.Fatalf("leftover temp must be ignored by listing: %d/%d", len(ents), len(probs))
	}
	// Retry after the simulated interruption succeeds.
	if p := mustWrite(t, s, rec(nil)); p == "" {
		t.Fatal("retry must succeed")
	}
	ents, _, _ = s.ListRecords("", "", now)
	if len(ents) != 1 {
		t.Fatalf("exactly one record expected, got %d", len(ents))
	}
}

// Scenario 11: a failed write (validation) leaves no partial state and a
// corrected retry succeeds; a completed write leaves no temp files behind.
func TestIntegrity_FailedWriteThenRetry(t *testing.T) {
	s := newStore(t)
	bad := rec(func(x *Record) { x.Result = "BOGUS" })
	if _, err := s.WriteRecord(bad, now); err == nil {
		t.Fatal("invalid record must be refused")
	}
	mustWrite(t, s, rec(nil))
	dir := filepath.Join(s.root, "wdtt", shaA)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind after successful write: %s", e.Name())
		}
	}
}

// Scenario 12 (store-level roundtrip): a synthetic-source PASS record never
// yields E2E evidence, even after a full write/read cycle.
func TestIntegrity_SyntheticEvidenceStoreRoundtrip(t *testing.T) {
	s := newStore(t)
	r := rec(func(x *Record) { x.ResultSource = SourceEmulator; x.EnvironmentID = "guest-01" })
	mustWrite(t, s, r)
	got, err := s.ReadRecord("wdtt", shaA, recordIDOf(r), now)
	if err != nil {
		t.Fatal(err)
	}
	lvl, _ := EffectiveEvidence(got, now, 0)
	if lvl == "E2E_VERIFIED" {
		t.Fatal("synthetic record must never promote to E2E_VERIFIED")
	}
	if lvl != "BUILD_CONFIRMED" {
		t.Fatalf("emulator-source fresh PASS must cap at BUILD_CONFIRMED, got %s", lvl)
	}
}

// Defense in depth: paths leaving the canonical root are refused even if a
// future code path manages to produce one.
func TestIntegrity_WithinRootDefense(t *testing.T) {
	s := newStore(t)
	if err := s.withinRoot(filepath.Join(s.root, "wdtt", "x.json")); err != nil {
		t.Fatalf("inside-root path must pass: %v", err)
	}
	if err := s.withinRoot(filepath.Join(s.root, "..", "evil.json")); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("escape must fail with ErrUnsafePath, got %v", err)
	}
}

// Existing regular files/dirs in unexpected roles are damaged elements:
// a file where a directory is expected aborts safely.
func TestIntegrity_FileWhereDirExpected(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Join(s.root, "wdtt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "wdtt", shaA), []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.WriteRecord(rec(nil), now)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("file where a sha dir belongs must refuse with ErrUnsafePath, got %v", err)
	}
}

// Root canonicalisation: opening through a symlinked path pins the REAL
// root as the trust boundary and keeps working.
func TestIntegrity_RootSymlinkCanonicalised(t *testing.T) {
	canSymlink(t)
	real := filepath.Join(t.TempDir(), "real-store")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link-to-store")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(link)
	if err != nil {
		t.Fatal(err)
	}
	if s.root != real {
		t.Fatalf("trust boundary must be the canonical root %q, got %q", real, s.root)
	}
	if p := mustWrite(t, s, rec(nil)); p == "" {
		t.Fatal("write through canonicalised root must succeed")
	}
}
