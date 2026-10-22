package wdtt

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"os"
	"testing"
)

// writeTestELF writes a minimal, structurally valid arm64 ELF (64-byte
// header, no sections) so internal/artifact can hash and parse it.
func writeTestELF(t *testing.T, path string) string {
	t.Helper()
	var hdr [64]byte
	copy(hdr[0:4], "\x7fELF")
	hdr[4] = 2                                     // ELFCLASS64
	hdr[5] = 1                                     // ELFDATA2LSB
	hdr[6] = 1                                     // EV_CURRENT
	binary.LittleEndian.PutUint16(hdr[16:18], 2)   // e_type = ET_EXEC
	binary.LittleEndian.PutUint16(hdr[18:20], 183) // e_machine = EM_AARCH64
	binary.LittleEndian.PutUint32(hdr[20:24], 1)   // e_version
	binary.LittleEndian.PutUint16(hdr[52:54], 64)  // e_ehsize
	binary.LittleEndian.PutUint16(hdr[54:56], 56)  // e_phentsize
	if err := os.WriteFile(path, hdr[:], 0o700); err != nil {
		t.Fatal(err)
	}
	return sha256File(t, path)
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

type netListener = net.Listener

func listenTCP(bind string) (netListener, error) {
	return net.Listen("tcp", bind)
}
