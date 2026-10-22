package artifact

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mockELF(arch string) []byte {
	size := 64
	if arch == "mipsle" {
		size = 52
	}
	b := make([]byte, size)
	copy(b[:4], []byte{0x7f, 'E', 'L', 'F'})
	if arch == "mipsle" {
		b[4] = byte(elf.ELFCLASS32)
	} else {
		b[4] = byte(elf.ELFCLASS64)
	}
	b[5] = byte(elf.ELFDATA2LSB)
	b[6] = 1
	binary.LittleEndian.PutUint16(b[16:18], uint16(elf.ET_EXEC))
	if arch == "mipsle" {
		binary.LittleEndian.PutUint16(b[18:20], uint16(elf.EM_MIPS))
	} else {
		binary.LittleEndian.PutUint16(b[18:20], uint16(elf.EM_AARCH64))
	}
	binary.LittleEndian.PutUint32(b[20:24], 1)
	if arch == "mipsle" {
		binary.LittleEndian.PutUint16(b[40:42], 52)
	} else {
		binary.LittleEndian.PutUint16(b[52:54], 64)
	}
	return b
}
func writeFixture(t *testing.T, content []byte) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(content)
	return path, hex.EncodeToString(h[:])
}
func TestVerifyWDTTValidELF(t *testing.T) {
	for _, arch := range []string{"arm64", "mipsle"} {
		t.Run(arch, func(t *testing.T) {
			p, sha := writeFixture(t, mockELF(arch))
			r, e := VerifyWDTT(p, sha, arch)
			if e != nil {
				t.Fatal(e)
			}
			if r.Verification != "HASH_AND_ARCH_MATCH" || r.TCPE2E != "NOT_TESTED" || r.UDPE2E != "NOT_TESTED" {
				t.Fatalf("bad report: %+v", r)
			}
		})
	}
}
func TestVerifyWDTTNegatives(t *testing.T) {
	p, sha := writeFixture(t, mockELF("arm64"))
	cases := []struct{ name, path, hash, arch string }{
		{"wrong-hash", p, strings.Repeat("0", 64), "arm64"},
		{"bad-hash", p, "abc", "arm64"},
		{"wrong-arch", p, sha, "mipsle"},
		{"unknown-arch", p, sha, "x86_64"},
		{"missing", p + "-missing", sha, "arm64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := VerifyWDTT(tc.path, tc.hash, tc.arch)
			if err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	nonELF, h := writeFixture(t, []byte(strings.Repeat("x", 64)))
	if _, err := VerifyWDTT(nonELF, h, "arm64"); err == nil {
		t.Fatal("non-ELF accepted")
	}
	symlink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(p, symlink); err == nil {
		if _, err = VerifyWDTT(symlink, sha, "arm64"); err == nil {
			t.Fatal("symlink accepted")
		}
	}
}
