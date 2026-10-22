package harness

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"time"

	"github.com/saymer-alt/keenwhite/internal/provider"
	"github.com/saymer-alt/keenwhite/internal/provider/wdtt"
)

// TempDir gives every scenario an isolated scratch directory.
func (h *H) TempDir() string {
	if h.tmpDir == "" {
		dir, err := os.MkdirTemp("", "keenwhite-harness-")
		if err != nil {
			h.Failf("tempdir: %v", err)
			return ""
		}
		h.tmpDir = dir
	}
	return h.tmpDir
}

// setRealELF writes a minimal valid arm64 ELF over the placeholder binary
// (no compiler needed; internal/artifact only parses the header).
func setRealELF(h *H, m *provider.Manifest) {
	var hdr [64]byte
	copy(hdr[0:4], "\x7fELF")
	hdr[4] = 2 // ELFCLASS64
	hdr[5] = 1 // ELFDATA2LSB
	hdr[6] = 1 // EV_CURRENT
	binary.LittleEndian.PutUint16(hdr[16:18], 2)
	binary.LittleEndian.PutUint16(hdr[18:20], 183)
	binary.LittleEndian.PutUint32(hdr[20:24], 1)
	binary.LittleEndian.PutUint16(hdr[52:54], 64)
	binary.LittleEndian.PutUint16(hdr[54:56], 56)
	if err := os.WriteFile(m.Artifact.Path, hdr[:], 0o700); err != nil {
		h.Failf("elf: %v", err)
	}
}

func shaOf(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

func listenOn(bind string) (net.Listener, error) { return net.Listen("tcp", bind) }

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// prepareOK runs Prepare on a fully valid fixture; nil on failure (already
// reported via h).
func prepareOK(h *H, env *scenarioEnv) *wdtt.Adapter {
	setRealELF(h, &env.manifest)
	env.manifest.Artifact.SHA256 = shaOf(env.manifest.Artifact.Path)
	a, err := env.adapter()
	if err != nil {
		h.Failf("adapter: %v", err)
		return nil
	}
	if _, err := a.Prepare(context.Background()); err != nil {
		h.Failf("prepare: %v", err)
		return nil
	}
	return a
}

func waitForState(a *wdtt.Adapter, want provider.LifecycleState, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		st, _ := a.Status(context.Background())
		if st.State == want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
