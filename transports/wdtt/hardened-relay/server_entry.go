// SPDX-License-Identifier: GPL-3.0-or-later
// Relay-only entry point. Does not call upstream main, APIs, Telegram, exec,
// TUN, WG setup, NAT, sysctl or database writers. WG peers are pre-provisioned.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/sys/unix"
)

type relayConfig struct {
	Listen string `json:"listen"`
	WGEndpoint string `json:"wg_endpoint"`
	ServerPublic string `json:"server_public_key"`
	DNS string `json:"dns"`
	Database string `json:"database_file"`
}
func relayRead(path string) ([]byte, error) {
	bad := errors.New("PRIVATE_FILE_REJECTED")
	if !filepath.IsAbs(path) { return nil, bad }
	dir := filepath.Dir(path); real, e := filepath.EvalSymlinks(dir)
	if e != nil || real != dir { return nil, bad }
	var ds unix.Stat_t
	if unix.Lstat(dir, &ds) != nil || ds.Mode & 0777 != 0700 || int(ds.Uid) != os.Geteuid() { return nil, bad }
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil { return nil, bad }; f := os.NewFile(uintptr(fd), "private"); defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode & unix.S_IFMT != unix.S_IFREG || st.Mode & 0777 != 0600 || st.Nlink != 1 || int(st.Uid) != os.Geteuid() || st.Size > 65536 { return nil, bad }
	return io.ReadAll(io.LimitReader(f, 65537))
}
func relayDecode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw)); d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil { return e }
	if d.Decode(new(any)) != io.EOF { return errors.New("TRAILING_JSON") }; return nil
}
func relayLoadDB(path string) (*Database, error) {
	b, e := relayRead(path); if e != nil { return nil, e }
	var next Database
	if relayDecode(b, &next) != nil || next.MainPassword != "" || next.BotToken != "" || next.AdminID != "" || len(next.Devices) == 0 || len(next.Devices) > 4 || len(next.Passwords) == 0 || len(next.Passwords) > 4 { return nil, errors.New("DB_REJECTED") }
	for id, dev := range next.Devices {
		if dev == nil || !serverIdentityValid(id) || dev.DeviceID != id || net.ParseIP(dev.IP).To4() == nil { return nil, errors.New("DEVICE_REJECTED") }
		priv, e := base64.StdEncoding.DecodeString(dev.PrivKey); pub, e2 := base64.StdEncoding.DecodeString(dev.PubKey)
		if e != nil || e2 != nil || len(priv) != 32 || len(pub) != 32 { return nil, errors.New("KEY_REJECTED") }
		derived, e := curve25519.X25519(priv, curve25519.Basepoint)
		if e != nil || !bytes.Equal(derived, pub) { return nil, errors.New("KEY_MISMATCH") }
	}
	for password, ent := range next.Passwords {
		if len(password) < 16 || !serverNoSecretFields(password) || ent == nil || ent.ExpiresAt == 0 || ent.ExpiresAt > time.Now().Add(30*time.Minute).Unix() || ent.MaxDevices > 1 || len(ent.DeviceIDs) > 1 { return nil, errors.New("CREDENTIAL_REJECTED") }
		id := ent.DeviceID; if len(ent.DeviceIDs) == 1 { id = ent.DeviceIDs[0]; if ent.DeviceID != "" && ent.DeviceID != id { return nil, errors.New("BINDING_CONFLICT") } }
		if next.Devices[id] == nil { return nil, errors.New("BINDING_REJECTED") }
	}
	return &next, nil
}
func relayConfigValid(c relayConfig) bool {
	h, _, e3 := net.SplitHostPort(c.Listen); wh, _, e4 := net.SplitHostPort(c.WGEndpoint)
	if e3 != nil || e4 != nil || net.ParseIP(h).To4() == nil || net.ParseIP(wh).To4() == nil { return false }
	a, e := net.ResolveUDPAddr("udp4", c.Listen)
	w, e2 := net.ResolveUDPAddr("udp4", c.WGEndpoint)
	// Literal addresses avoid implicit DNS/control-plane bootstrap.
	k, e5 := base64.StdEncoding.DecodeString(c.ServerPublic)
	return e == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil && len(k) == 32 && net.ParseIP(h).To4() != nil && net.ParseIP(wh).To4() != nil && a.Port > 0 && w.Port > 0 && w.IP.IsLoopback() && net.ParseIP(c.DNS).To4() != nil
}
func relayRun(path string) error {
	raw, e := relayRead(path); if e != nil { return e }
	var c relayConfig
	if relayDecode(raw, &c) != nil || !relayConfigValid(c) { return errors.New("CONFIG_REJECTED") }
	next, e := relayLoadDB(c.Database); if e != nil { return e }
	db = next; dns = c.DNS
	passwords := []string{}
	for p, ent := range next.Passwords { if !ent.IsDeactivated && ent.ExpiresAt > time.Now().Unix() { passwords = append(passwords, p) } }
	if serverWrapKeys.SetPasswords("", passwords) != nil { return errors.New("WRAP_REJECTED") }
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT); defer cancel()
	ctx, limit := context.WithTimeout(ctx, 30*time.Minute); defer limit()
	cert, e := selfsign.GenerateSelfSigned(); if e != nil { return e }
	a, _ := net.ResolveUDPAddr("udp4", c.Listen)
	wrapped, e := listenWrapped(a, serverWrapKeys); if e != nil { return e }
	l, e := dtls.NewListenerWithOptions(wrapped, dtls.WithCertificates(cert), dtls.WithExtendedMasterSecret(dtls.RequireExtendedMasterSecret), dtls.WithCipherSuites(dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256), dtls.WithConnectionIDGenerator(dtls.RandomCIDGenerator(8)), dtls.WithMTU(1100))
	if e != nil { wrapped.Close(); return e }; defer l.Close()
	context.AfterFunc(ctx, func(){ l.Close() })
	// Atomic file replacement reload; invalid/missing DB cancels every session.
	go func(){
		t := time.NewTicker(250*time.Millisecond); defer t.Stop()
		for { select { case <-ctx.Done(): return; case <-t.C:
			n, err := relayLoadDB(c.Database); if err != nil { cancel(); return }
			dbMutex.Lock(); db = n; dbMutex.Unlock()
			ps := []string{}; for p, ent := range n.Passwords { if !ent.IsDeactivated && ent.ExpiresAt > time.Now().Unix() { ps = append(ps,p) } }
			if serverWrapKeys.SetPasswords("",ps) != nil { cancel(); return }
		} }
	}()
	slots := make(chan struct{},16)
	for { conn, err := l.Accept(); if err != nil { if ctx.Err()!=nil { return nil }; return err }
		select { case slots <- struct{}{}:
			go func(){ defer func(){ <-slots; conn.Close() }(); done := context.AfterFunc(ctx,func(){conn.Close()}); defer done(); handleConn(ctx,conn,c.WGEndpoint,nil,&wgKeys{serverPublic:c.ServerPublic}) }()
		default: conn.Close() }
	}
}
func main() {
	if len(os.Args)==2 && os.Args[1]=="-h" { fmt.Println("WDTT relay laboratory: -config-file /absolute/private/server.json; pre-provisioned WG; no TUN/NAT/API/Telegram; 30min cap"); return }
	if len(os.Args)!=3 || os.Args[1]!="-config-file" { fmt.Fprintln(os.Stderr,"WDTT_SERVER_CONFIG_REQUIRED"); os.Exit(78) }
	path := os.Args[2]
	if _,e := relayRead(path); e != nil { fmt.Fprintln(os.Stderr,"WDTT_SERVER_PRIVATE_FILE_REJECTED"); os.Exit(78) }
	unix.Umask(0077); if unix.Setrlimit(unix.RLIMIT_CORE,&unix.Rlimit{}) != nil { os.Exit(78) }
	sink,e := os.OpenFile("/dev/null",os.O_RDWR,0); if e != nil { os.Exit(78) }
	for _,fd := range []int{0,1,2} { if unix.Dup2(int(sink.Fd()),fd)!=nil { os.Exit(78) } }; sink.Close()
	log.SetOutput(io.Discard)
	if relayRun(path)!=nil { os.Exit(78) }
}
