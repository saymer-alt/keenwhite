package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Integrity errors (task 15 / issue #38). All are matchable via errors.Is.
var (
	// ErrRecordExists: the target record path already exists. Acceptance
	// records are append-only; a writer that loses the publication race
	// (another process published the same record id first) reports this
	// too. The existing record is NEVER modified or replaced.
	ErrRecordExists = errors.New("acceptance record already exists; records are append-only")
	// ErrIntegrity: a record's stored identity does not match its store
	// coordinates (moved, renamed or swapped record).
	ErrIntegrity = errors.New("acceptance record identity mismatch")
	// ErrUnsafePath: a path element is a symlink, not a directory, or the
	// path would leave the trusted store root.
	ErrUnsafePath = errors.New("unsafe acceptance store path")
)

// Store is a directory-backed offline record store. Layout:
//
//	<root>/<provider_id>/<artifact_sha256>/<record_id>.json
//
// Trust boundary (task 15 stage C): OpenStore canonicalises the root path
// ONCE (symlinks resolved) and the resolved path becomes the trusted root.
// Every record path is built from pattern-checked elements inside that
// root; every intermediate directory is re-checked with Lstat on each
// operation, so a directory swapped for a symlink after OpenStore is
// rejected instead of being written through.
//
// Publication (task 15 stage A): WriteRecord creates a private temp file
// in the target directory, fsyncs it, then publishes it with os.Link() —
// an atomic create-if-absent. If the target appeared concurrently, Link
// fails with EEXIST and the caller loses: no writer, in any process or
// Store instance, can replace or truncate another writer's record. No
// in-process mutex is involved. This requires a filesystem with hard-link
// support (NTFS, ext4, xfs, btrfs, APFS, ...); on filesystems without it
// WriteRecord returns a typed error instead of silently downgrading to an
// unsafe rename.
type Store struct{ root string }

// OpenStore returns a store rooted at dir (created with 0700 when missing).
// The canonical, symlink-resolved root is stored as the trust boundary.
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store root required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create store root: %w", err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve store root symlinks: %w", err)
	}
	if info, err := os.Lstat(real); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("store root is not a directory: %s", real)
	}
	return &Store{root: real}, nil
}

func (s *Store) recordPath(providerID, sha, recordID string) (string, error) {
	if !idPattern.MatchString(providerID) || !shaPattern.MatchString(sha) || !recordPattern.MatchString(recordID) {
		return "", fmt.Errorf("%w: invalid record coordinates", ErrUnsafePath)
	}
	return filepath.Join(s.root, providerID, sha, recordID+".json"), nil
}

// withinRoot is defense in depth: every produced path must stay inside the
// canonical root. Pattern checks already forbid traversal, so this can
// only fire if the root itself is malformed.
func (s *Store) withinRoot(path string) error {
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s escapes the store root", ErrUnsafePath, path)
	}
	return nil
}

// ensureDirSafe creates or verifies every directory component under the
// root WITHOUT following symlinks: a component that is a symlink (or not a
// directory) aborts the operation instead of being written through.
func (s *Store) ensureDirSafe(relSlash string) error {
	cur := s.root
	for _, part := range strings.Split(relSlash, "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: %s is a symlink", ErrUnsafePath, cur)
			}
			if !info.IsDir() {
				return fmt.Errorf("%w: %s is not a directory", ErrUnsafePath, cur)
			}
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(cur, 0o700); err != nil {
				if errors.Is(err, os.ErrExist) {
					// Lost a creation race: only a real directory wins.
					info, err = os.Lstat(cur)
					if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
						return fmt.Errorf("%w: %s", ErrUnsafePath, cur)
					}
					continue
				}
				return fmt.Errorf("create store directory: %w", err)
			}
		default:
			return fmt.Errorf("stat store path: %w", err)
		}
	}
	return nil
}

// syncDir best-effort fsyncs a directory so a published record survives a
// crash together with its directory entry. Not every platform or filesystem
// supports directory fsync (e.g. Windows); errors are deliberately ignored
// there. The record file itself is always fsynced BEFORE publication.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}

// WriteRecord validates the record and stores it atomically. An existing
// record with the same ID is never overwritten; concurrent writers with
// the same record id produce exactly one winner and ErrRecordExists
// losers, across processes and Store instances (atomic link publication).
func (s *Store) WriteRecord(r Record, now time.Time) (string, error) {
	if findings := ValidateRecord(r, now); len(findings) > 0 {
		return "", fmt.Errorf("invalid acceptance record: %s: %s",
			findings[0].Field, findings[0].Message)
	}
	id := recordIDOf(r)
	path, err := s.recordPath(r.ProviderID, r.ArtifactSHA256, id)
	if err != nil {
		return "", err
	}
	if err := s.withinRoot(path); err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	rel, err := filepath.Rel(s.root, dir)
	if err != nil {
		return "", fmt.Errorf("%w: cannot relativize %s", ErrUnsafePath, dir)
	}
	if err := s.ensureDirSafe(filepath.ToSlash(rel)); err != nil {
		return "", err
	}
	// Advisory pre-check for a readable error message. The authoritative
	// guard is the atomic publish below: there is deliberately no
	// check-then-act window whose outcome decides the race.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %s is a symlink", ErrUnsafePath, path)
		}
		return "", fmt.Errorf("%w: %s", ErrRecordExists, path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat record: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode record: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp record: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once published
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("chmod temp record: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temp record: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sync temp record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp record: %w", err)
	}
	// Atomic create-if-absent publication: link() fails with EEXIST when
	// any other writer has already published this exact record id.
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%w: %s (another writer published first)", ErrRecordExists, path)
		}
		return "", fmt.Errorf("publish record (filesystem must support hard links): %w", err)
	}
	syncDir(dir)
	return path, nil
}

// recordIDOf derives the deterministic record ID from the record content:
// test type, second-precision measurement time and profile label. Two runs
// of the same test in the same second collide on purpose — the atomic
// publish makes one of them the single winner (evidence is not silently
// versioned).
func recordIDOf(r Record) string {
	return fmt.Sprintf("%s-%s-%s", strings.ToLower(r.TestType),
		r.MeasuredAt.UTC().Format("20060102t150405"), labelSlug(r.ProfileLabel))
}

func labelSlug(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if len(out) > 24 {
		out = out[:24]
	}
	return strings.Trim(out, "-.")
}

// checkStoredIdentity (task 15 stage B): the record content must match the
// store coordinates it was found under. A moved, copied-to-another-path or
// swapped record is an integrity failure, never silently accepted.
func checkStoredIdentity(r Record, providerID, sha, recordID string) error {
	if r.ProviderID != providerID ||
		!strings.EqualFold(r.ArtifactSHA256, sha) ||
		recordIDOf(r) != recordID {
		return fmt.Errorf("%w: stored identity does not match its path (provider dir %q, sha dir %q, file %q)",
			ErrIntegrity, providerID, sha, recordID)
	}
	return nil
}

// ReadRecord reads and re-validates one record. A corrupt, symlinked or
// identity-mismatched file is a typed error, never a silent zero value.
func (s *Store) ReadRecord(providerID, sha, recordID string, now time.Time) (Record, error) {
	var zero Record
	path, err := s.recordPath(providerID, sha, recordID)
	if err != nil {
		return zero, err
	}
	if err := s.withinRoot(path); err != nil {
		return zero, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return zero, fmt.Errorf("open record: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return zero, fmt.Errorf("%w: %s is not a regular record file", ErrUnsafePath, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read record: %w", err)
	}
	r, err := decodeRecord(b)
	if err != nil {
		return zero, err
	}
	if findings := ValidateRecord(r, now); len(findings) > 0 {
		return zero, fmt.Errorf("record failed validation: %s: %s", findings[0].Field, findings[0].Message)
	}
	if err := checkStoredIdentity(r, providerID, sha, recordID); err != nil {
		return zero, err
	}
	return r, nil
}

// Entry is one listed record with its store coordinates.
type Entry struct {
	ProviderID  string
	ArtifactSHA string
	RecordID    string
	Record      Record
}

// Problem describes an unreadable store entry; corrupt, symlinked or
// identity-mismatched records are REPORTED, never silently skipped (they
// may indicate tampering).
type Problem struct {
	Path   string
	Reason string
}

// ListRecords walks the store and returns valid entries plus problems.
// Selectors narrow the walk ("" = unrestricted).
func (s *Store) ListRecords(providerID, sha string, now time.Time) ([]Entry, []Problem, error) {
	var (
		entries  []Entry
		problems []Problem
	)
	base := s.root
	if providerID != "" {
		if !idPattern.MatchString(providerID) {
			return nil, nil, fmt.Errorf("%w: invalid provider selector", ErrUnsafePath)
		}
		base = filepath.Join(s.root, providerID)
	}
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		// Never follow or accept symlinked record files (WalkDir does not
		// descend into them; a symlink with a .json name is still a hazard).
		if d.Type()&os.ModeSymlink != 0 {
			problems = append(problems, Problem{path, "symlinked record file"})
			return nil
		}
		rel, relErr := filepath.Rel(s.root, path)
		if relErr != nil {
			problems = append(problems, Problem{path, "outside store root"})
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 3 {
			problems = append(problems, Problem{path, "unexpected layout"})
			return nil
		}
		if sha != "" && !strings.EqualFold(parts[1], sha) {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			problems = append(problems, Problem{path, readErr.Error()})
			return nil
		}
		rec, decErr := decodeRecord(b)
		if decErr != nil {
			problems = append(problems, Problem{path, decErr.Error()})
			return nil
		}
		if vf := ValidateRecord(rec, now); len(vf) > 0 {
			problems = append(problems, Problem{path, "validation: " + vf[0].Field + ": " + vf[0].Message})
			return nil
		}
		recordID := strings.TrimSuffix(parts[2], ".json")
		if err := checkStoredIdentity(rec, parts[0], parts[1], recordID); err != nil {
			problems = append(problems, Problem{path, err.Error()})
			return nil
		}
		entries = append(entries, Entry{
			ProviderID: parts[0], ArtifactSHA: parts[1],
			RecordID: recordID, Record: rec,
		})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Record.MeasuredAt.Before(entries[j].Record.MeasuredAt)
	})
	return entries, problems, nil
}

// MaxRecords caps how many records callers should iterate; the store itself
// returns everything but documents the bound here for tooling.
const MaxRecords = 4096
