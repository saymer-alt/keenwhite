// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"crypto/tls"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"net"
	"testing"
	"time"
)

// Runs the actual upstream connection handler, never main/startUserspaceWG/NAT.
// No TURN/WRAP here: test the authenticated-DTLS boundary in isolation.
func TestRejectWrongAUTH(t *testing.T) { testAUTH(t, "AUTH:fixture|wrong-password", false) }
func TestAcceptKnownAUTH(t *testing.T) { testAUTH(t, "AUTH:fixture|synthetic-owner-password", true) }
func TestRejectUnknownDevice(t *testing.T) {
	testAUTH(t, "AUTH:unknown|synthetic-owner-password", false)
}
func TestRejectNoAUTH(t *testing.T) { testAUTH(t, "WIREGUARD-SHAPED-FIXTURE", false) }
func testAUTH(t *testing.T, first string, wantForward bool) {
	testAUTHDB(t, first, wantForward, nil)
}
func testAUTHDB(t *testing.T, first string, wantForward bool, setup func()) {
	db = &Database{MainPassword: "synthetic-owner-password", Devices: map[string]*ClientDevice{"fixture": {DeviceID: "fixture"}}, Passwords: map[string]*PasswordEntry{}}
	if setup != nil { setup() }
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := dtls.Listen("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, &dtls.Config{Certificates: []tls.Certificate{cert}, ExtendedMasterSecret: dtls.RequireExtendedMasterSecret, CipherSuites: []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, e := listener.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		handleConn(ctx, c, udp.LocalAddr().String(), nil, nil)
	}()
	client, err := dtls.Dial("udp4", listener.Addr().(*net.UDPAddr), &dtls.Config{InsecureSkipVerify: true, ExtendedMasterSecret: dtls.RequireExtendedMasterSecret, CipherSuites: []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hc, hcancel := context.WithTimeout(ctx, 5*time.Second)
	defer hcancel()
	if client.HandshakeContext(hc) != nil {
		t.Fatal("DTLS handshake failed")
	}
	if _, err = client.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	if first != "WIREGUARD-SHAPED-FIXTURE" {
		_, _ = client.Write([]byte("WIREGUARD-SHAPED-FIXTURE"))
	}
	udp.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 2048)
	n, _, readErr := udp.ReadFrom(buf)
	forwarded := readErr == nil && string(buf[:n]) == "WIREGUARD-SHAPED-FIXTURE"
	if forwarded != wantForward {
		t.Errorf("AUTH boundary forwarding=%v want=%v", forwarded, wantForward)
	}
	cancel()
	client.Close()
	listener.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handler did not stop")
	}
}
