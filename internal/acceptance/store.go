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

// Store is a directory-backed offline record store. Layout:
//
//	<root>/<provider_id>/<artifact_sha256>/<record_id>.json
//
// Every path element is pattern-checked, so traversal is impossible by
// construction. Writes are atomic (temp file + rename) and refuse to
// overwrite: acceptance records are append-only evidence — rewriting
// history is tampering, not bookkeeping.
type Store struct{ root string }

// OpenStore returns a store rooted at dir (created with 0700 when missing).
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("store root required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create store root: %w", err)
	}
	return &Store{root: dir}, nil
}

func (s *Store) recordPath(providerID, sha, recordID string) (string, error) {
	if !idPattern.MatchString(providerID) || !shaPattern.MatchString(sha) || !recordPattern.MatchString(recordID) {
		return "", errors.New("invalid store path element")
	}
	return filepath.Join(s.root, providerID, sha, recordID+".json"), nil
}

// WriteRecord validates the record and stores it atomically. An existing
// record with the same ID is never overwritten (append-only evidence).
func (s *Store) WriteRecord(r Record, now time.Time) (string, error) {
	if findings := ValidateRecord(r, now); len(findings) > 0 {
		return "", fmt.Errorf("invalid acceptance record: %s: %s",
			findings[0].Field, findings[0].Message)
	}
	path, err := s.recordPath(r.ProviderID, r.ArtifactSHA256, recordIDOf(r))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("record %s already exists; acceptance records are append-only", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat record: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create record directory: %w", err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode record: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp record: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
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
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("commit record: %w", err)
	}
	return path, nil
}

// recordIDOf derives the deterministic record ID from the record content:
// test type, minute-precision measurement time and profile label. Two runs
// of the same test in the same minute collide on purpose — the caller must
// use distinct profile labels or wait (evidence is not silently versioned).
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

// ReadRecord reads and re-validates one record. A corrupt or tampered file
// is a typed error, never a silent zero value.
func (s *Store) ReadRecord(providerID, sha, recordID string, now time.Time) (Record, error) {
	path, err := s.recordPath(providerID, sha, recordID)
	if err != nil {
		return Record{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Record{}, fmt.Errorf("open record: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Record{}, errors.New("record path is not a regular file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("read record: %w", err)
	}
	r, err := decodeRecord(b)
	if err != nil {
		return Record{}, err
	}
	if findings := ValidateRecord(r, now); len(findings) > 0 {
		return Record{}, fmt.Errorf("record failed validation: %s: %s", findings[0].Field, findings[0].Message)
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

// Problem describes an unreadable store entry; corrupt records are
// REPORTED, never silently skipped (they may indicate tampering).
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
			return nil, nil, errors.New("invalid provider selector")
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
		entries = append(entries, Entry{
			ProviderID: parts[0], ArtifactSHA: parts[1],
			RecordID: strings.TrimSuffix(parts[2], ".json"), Record: rec,
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
