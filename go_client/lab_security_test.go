// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const canary = "SYNTHETIC_PRIVATE_KEY_COOKIE_PASSWORD_DO_NOT_LOG"

func TestLeakHelper(t *testing.T) {
	if os.Getenv("WDTT_TEST_CHILD") != "1" {
		return
	}
	if labSilence() != nil {
		os.Exit(1)
	}
	fmt.Println(canary)
	fmt.Fprintln(os.Stderr, canary)
	log.Print(canary)
	log.New(os.Stderr, "vendor", 0).Print(canary)
	_, _ = os.Stdout.WriteString(canary)
	os.Exit(0)
}
func TestAllDiagnosticSinks(t *testing.T) {
	c := exec.Command(os.Args[0], "-test.run=^TestLeakHelper$")
	c.Env = append(os.Environ(), "WDTT_TEST_CHILD=1")
	out, err := c.CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatal("untrusted diagnostic sink escaped")
	}
}
func TestPrivateFilesAtomicAndSymlink(t *testing.T) {
	labDir = t.TempDir()
	os.Chmod(labDir, 0700)
	if err := labWrite("wg-turn.conf", []byte(canary)); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(labDir, "wg-turn.conf")
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal("unsafe config mode")
	}
	if err := labWrite("wg-turn.conf", []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	os.Chmod(p, 0644)
	if labWrite("wg-turn.conf", nil) == nil {
		t.Fatal("accepted foreign permissions")
	}
	os.Remove(p)
	target := filepath.Join(labDir, "untouched")
	os.WriteFile(target, []byte(canary), 0600)
	os.Symlink(target, p)
	if labWrite("wg-turn.conf", nil) == nil {
		t.Fatal("followed symlink")
	}
	b, _ := os.ReadFile(target)
	if string(b) != canary {
		t.Fatal("changed foreign target")
	}
	os.Remove(p)
	os.Link(target, p)
	if _, err := labRead(p); err == nil {
		t.Fatal("accepted hard link")
	}
	os.Remove(p)
	os.WriteFile(p, []byte(canary), 0600)
	os.Chmod(labDir, 0755)
	if _, err := labRead(p); err == nil {
		t.Fatal("accepted public directory")
	}
}
func TestEndpointExclusive(t *testing.T) {
	a, err := listenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := listenUDP(a.LocalAddr().String())
	if err == nil {
		b.Close()
		t.Fatal("shared endpoint")
	}
}
func TestGETCONFAndAUTHWireFixture(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	conf := "[Interface]\nPrivateKey = " + canary + "\nAddress = 10.77.0.2/32\n[Peer]\nEndpoint = 127.0.0.1:9000\n"
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		n, e := b.Read(buf)
		if e == nil && string(buf[:n]) != "GETCONF:9000|test|"+canary {
			e = fmt.Errorf("wire mismatch")
		}
		if e == nil {
			_, e = b.Write([]byte(conf))
		}
		done <- e
	}()
	got, err := RequestConfig(a, "9000", "test", canary)
	if err != nil || got != conf {
		t.Fatal("GETCONF fixture failed")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		n, e := b.Read(buf)
		if e == nil && !bytes.Equal(buf[:n], []byte("AUTH:test|"+canary)) {
			e = fmt.Errorf("AUTH mismatch")
		}
		done <- e
	}()
	if SendAuth(a, "test", canary) != nil {
		t.Fatal("AUTH write failed")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	// This is a protocol fixture, not an upstream server/VK/WireGuard acceptance.
	if strings.Contains(conf, "PostUp") {
		t.Fatal("invalid fixture")
	}
}
