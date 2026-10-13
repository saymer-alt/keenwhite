// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/cbeuw/connutil"
)

// Fixed per-direction budgets, including empty packets. 64 KiB accommodates a
// complete maximum UDP datagram; 32 records allow a 35.2 KiB DTLS-MTU burst.
// No payload allocation, capacity growth, or per-wait timer/goroutine.
const packetPipeBytes = 64 * 1024
const packetPipePackets = 32

type packetQueue struct {
	mu                         sync.Mutex
	cond                       *sync.Cond
	data                       [packetPipeBytes]byte
	lengths                    [packetPipePackets]int
	offset, used, first, count int
	closed                     bool
	rd, wd                     time.Time
	rt, wt                     *time.Timer
}

type boundedPacketPipe struct{ read, write *packetQueue }

func newPacketQueue() *packetQueue {
	q := new(packetQueue)
	q.cond = sync.NewCond(&q.mu)
	return q
}
func newBoundedPacketPipe() (*boundedPacketPipe, *boundedPacketPipe) {
	a, b := newPacketQueue(), newPacketQueue()
	return &boundedPacketPipe{a, b}, &boundedPacketPipe{b, a}
}
func expired(t time.Time) bool { return !t.IsZero() && !time.Now().Before(t) }
func (p *boundedPacketPipe) WriteTo(b []byte, _ net.Addr) (int, error) {
	q := p.write
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(b) > packetPipeBytes {
		return 0, connutil.ErrWriteToLarge
	}
	for {
		if q.closed {
			return 0, io.ErrClosedPipe
		}
		if expired(q.wd) {
			return 0, os.ErrDeadlineExceeded
		}
		if q.count < packetPipePackets && q.used+len(b) <= packetPipeBytes {
			break
		}
		q.cond.Wait()
	}
	at := (q.offset + q.used) % packetPipeBytes
	n := copy(q.data[at:], b)
	copy(q.data[:], b[n:])
	q.lengths[(q.first+q.count)%packetPipePackets] = len(b)
	q.used += len(b)
	q.count++
	q.cond.Broadcast()
	return len(b), nil
}
func (p *boundedPacketPipe) ReadFrom(b []byte) (int, net.Addr, error) {
	q := p.read
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		if q.closed {
			return 0, p.LocalAddr(), io.ErrClosedPipe
		}
		if expired(q.rd) {
			return 0, p.LocalAddr(), os.ErrDeadlineExceeded
		}
		if q.count > 0 {
			break
		}
		q.cond.Wait()
	}
	size := q.lengths[q.first]
	if size > len(b) {
		return 0, p.LocalAddr(), io.ErrShortBuffer
	}
	n := copy(b[:size], q.data[q.offset:])
	copy(b[n:size], q.data[:size-n])
	q.offset = (q.offset + size) % packetPipeBytes
	q.used -= size
	q.first = (q.first + 1) % packetPipePackets
	q.count--
	q.cond.Broadcast()
	return size, p.LocalAddr(), nil
}
func (q *packetQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.count = 0
	q.used = 0
	if q.rt != nil {
		q.rt.Stop()
	}
	if q.wt != nil {
		q.wt.Stop()
	}
	q.cond.Broadcast()
}
func (p *boundedPacketPipe) Close() error { p.read.close(); p.write.close(); return nil }
func (q *packetQueue) deadline(t time.Time, read bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	timer := &q.wt
	if read {
		q.rd = t
		timer = &q.rt
	} else {
		q.wd = t
	}
	if *timer != nil {
		(*timer).Stop()
		*timer = nil
	}
	if !q.closed && !t.IsZero() && time.Until(t) > 0 {
		*timer = time.AfterFunc(time.Until(t), func() { q.mu.Lock(); q.cond.Broadcast(); q.mu.Unlock() })
	}
	q.cond.Broadcast()
}
func (p *boundedPacketPipe) SetReadDeadline(t time.Time) error { p.read.deadline(t, true); return nil }
func (p *boundedPacketPipe) SetWriteDeadline(t time.Time) error {
	p.write.deadline(t, false)
	return nil
}
func (p *boundedPacketPipe) SetDeadline(t time.Time) error {
	p.SetReadDeadline(t)
	p.SetWriteDeadline(t)
	return nil
}
func (p *boundedPacketPipe) LocalAddr() net.Addr { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)} }

var _ net.PacketConn = (*boundedPacketPipe)(nil)
