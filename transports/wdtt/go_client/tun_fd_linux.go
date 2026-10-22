//go:build linux

package main

import (
	"net"
	"os"
	"time"
)

// fdPacketConn — plain IP read/write on TUN fd (как Android VPNService fd).
type fdPacketConn struct {
	f    *os.File
	name string
}

func newFdPacketConn(f *os.File, name string) *fdPacketConn {
	return &fdPacketConn{f: f, name: name}
}

func (c *fdPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.f.Read(p)
	if err != nil {
		return 0, nil, err
	}
	return n, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}, nil
}

func (c *fdPacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return c.f.Write(p)
}

func (c *fdPacketConn) Close() error                       { return c.f.Close() }
func (c *fdPacketConn) LocalAddr() net.Addr                { return &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (c *fdPacketConn) SetDeadline(t time.Time) error      { return c.f.SetDeadline(t) }
func (c *fdPacketConn) SetReadDeadline(t time.Time) error  { return c.f.SetReadDeadline(t) }
func (c *fdPacketConn) SetWriteDeadline(t time.Time) error { return c.f.SetWriteDeadline(t) }
