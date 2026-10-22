// Package artifact performs offline read-only provenance checks on router binaries.
// It never executes a binary, starts a tunnel, or touches routing and credentials.
package artifact

import (
	"crypto/sha256"
	"crypto/subtle"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const MaxSize = 256 << 20

type Report struct {
	Provider     string `json:"provider"`
	Architecture string `json:"architecture"`
	SHA256       string `json:"sha256"`
	Format       string `json:"format"`
	Verification string `json:"verification"`
	TCPE2E       string `json:"tcp_e2e"`
	UDPE2E       string `json:"udp_e2e"`
}

// VerifyWDTT checks that a regular ELF file matches an independently supplied
// SHA256 and requested router architecture. SHA match is not evidence of E2E.
func VerifyWDTT(path, expectedHash, architecture string) (Report, error) {
	var empty Report
	if architecture != "arm64" && architecture != "mipsle" {
		return empty, errors.New("architecture must be arm64 or mipsle")
	}
	if len(expectedHash) != 64 {
		return empty, errors.New("expected SHA256 must have exactly 64 hexadecimal characters")
	}
	expected, err := hex.DecodeString(expectedHash)
	if err != nil {
		return empty, errors.New("expected SHA256 is not hexadecimal")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return empty, fmt.Errorf("stat artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return empty, errors.New("artifact symlinks are not allowed")
	}
	if !info.Mode().IsRegular() {
		return empty, errors.New("artifact must be a regular file")
	}
	if info.Size() < 52 || info.Size() > MaxSize {
		return empty, errors.New("artifact size out of safety bounds")
	}
	f, err := os.Open(path)
	if err != nil {
		return empty, fmt.Errorf("open artifact: %w", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return empty, fmt.Errorf("fstat artifact: %w", err)
	}
	if !opened.Mode().IsRegular() || opened.Size() != info.Size() {
		return empty, errors.New("artifact changed or is not regular")
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return empty, fmt.Errorf("hash artifact: %w", err)
	}
	if n != opened.Size() {
		return empty, errors.New("artifact changed during hashing")
	}
	got := h.Sum(nil)
	if subtle.ConstantTimeCompare(got, expected) != 1 {
		return empty, errors.New("artifact SHA256 mismatch")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return empty, fmt.Errorf("seek artifact: %w", err)
	}
	binary, err := elf.NewFile(f)
	if err != nil {
		return empty, fmt.Errorf("invalid ELF artifact: %w", err)
	}
	defer binary.Close()
	if binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN {
		return empty, errors.New("ELF is not executable or PIE")
	}
	if binary.Data != elf.ELFDATA2LSB {
		return empty, errors.New("ELF is not little-endian")
	}
	switch architecture {
	case "arm64":
		if binary.Machine != elf.EM_AARCH64 || binary.Class != elf.ELFCLASS64 {
			return empty, errors.New("ELF machine/class mismatch for arm64")
		}
	case "mipsle":
		if binary.Machine != elf.EM_MIPS || binary.Class != elf.ELFCLASS32 {
			return empty, errors.New("ELF machine/class mismatch for mipsle")
		}
	}
	return Report{
		Provider: "wdtt", Architecture: architecture, SHA256: strings.ToLower(expectedHash),
		Format: "ELF", Verification: "HASH_AND_ARCH_MATCH", TCPE2E: "NOT_TESTED", UDPE2E: "NOT_TESTED",
	}, nil
}
