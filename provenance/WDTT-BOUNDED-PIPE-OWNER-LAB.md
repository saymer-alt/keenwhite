# Bounded packet pipe — Issue #22 (OWNER LAB ONLY)

Stacked on immutable PR #21 `f04bdbc101c8f9f86c2cc6a1cc512e9d2373504c`.
No admission on 512 MiB hardware. No router/VPS deployment or release.

## Why not LimitedAsyncPacketPipe

Locked connutil v1.0.1 `AsyncPacketPipe` delegates to limit=0. Its limited
variant checks `buf.Len() <= softLimit` before admitting an entire packet;
there is one-packet overshoot and bytes.Buffer backing capacity is not the
logical byte count. Empty packets grow pLens independently of the byte limit.
`TestLimitedPipeZeroPacketReproducer` confirms 100000 empty records are
accepted with limit=1600 and no reader. This is not a strict memory bound.
The dependency also creates an unstopped AfterFunc on every deadline wait.

## Narrow replacement and ownership

`packet_pipe.go` implements only the net.PacketConn contract needed by DTLS.
Two private fixed rings per session: **65536 bytes + 32 integer lengths per
direction**; no payload allocations, growing slice, per-wait timer or goroutine.
The byte array holds the maximum UDP datagram; the packet ring permits a burst
of 32 DTLS records (MTU 1100, 35200 bytes). Both budgets are checked before
copying; no one-packet overshoot and empty packets occupy a record slot.
On 64-bit Go the arrays alone use 131584 bytes/worker: 1184256 for 9,
2368512 for 18. Mutex/condition/deadline/timer and allocator size-class overhead
are additional; these are **pipe-only**, not total-client RAM guarantees.

Read preserves packet boundaries and reports io.ErrShortBuffer without consuming
the packet. Write copies caller bytes before returning; full queues block, never
silently drop. Close discards queued data and unblocks both directions; no graceful
drain is promised after cancellation. The fixed max write is 65536 bytes, above
the legal UDP payload limit and existing 1600/1680 relay read buffers. No wire
framing, cipher, DTLS MTU or SOCKS limits change.

Each queue has at most one active read and one active write deadline timer;
setters stop the previous timer, callbacks lock the condition mutex, Close stops
timers. Wait loops recheck deadline/closed state on every wakeup. No timer is
created by entering a wait. Concurrent operations share the bounded rings.

RunSession and RunPing use the same implementation. Context cancellation closes
relay and pipe, early returns join relay pumps (and session keepalive), and errors
from ping forwarding now terminate the pump. Pipe closure precedes DTLS Close
so a close-notify cannot wait on a full queue. Context cancellation also closes
the TURN transaction table/socket; RunPing has an overall 15-second context in
addition to its handshake timeout. Existing system DNS resolution is not made
context-aware by this change.

## Reproduction

Use the pinned Entware Go 1.26.1 toolchain already used by #21:

```sh
cd go_client
CGO_ENABLED=0 "$GO_BIN" test -mod=readonly -short -count=1 ./...
CGO_ENABLED=1 CC=gcc "$GO_BIN" test -mod=readonly -race -short -count=1 ./...
cd ..
sudo python3 -B tools/check_bounded_pipe_lab.py --go "$GO_BIN" \
  --work /private/new-pipe-lab --report-dir /private/new-numeric-report
```

Run only in a disposable Linux lab. The A/B tool archives the exact baseline
runtime and overlays the identical pressure test, replacing only its pipe factory.
It checks source cleanliness, records toolchain/SHAs and binary SHA256, pins two
CPUs, runs three independent processes per variant and 9/18 plan. Synthetic
backlog offers 4096 x 1600-byte records per direction, pauses consumption for two
seconds then uses a slow consumer for one second. Accepted bytes differ because
backpressure is the intended behavior; this is a stalled-queue memory comparison,
not a throughput-equivalent comparison. Normal transport runs use identical
existing local TURN/DTLS/WG/SOCKS TCP/UDP fixtures and report delivered bytes,
latency, peak RSS/VmHWM, heap, goroutines and FDs. Only lo inside fresh namespaces
is configured; no production endpoint, host route/firewall or VK credentials.

## Verification status

- Native unit/race: PASS locally, including dependency reproducer, packet sizes
  0..65536, short/oversize records, both-direction pressure, 100 stop/deadline/cancel
  cycles, 9/18 slow consumers and cancelled TURN allocation in RunPing.
- ARM64 and MIPSel softfloat QEMU: PASS locally; ELF and SHA256 recorded separately.
- Paired numeric memory and full transport: running; numerical results will be
  added after completion. No allocator-only memory qualification claimed.
- Entware acceptance and independent CI: pending this exact candidate.
- Shipping 30-minute emergency exit 75, RAM floors, SOCKS 32/64 and UDP 4 caps,
  protocol/WRAP goldens are retained. Short lab tests do not prove real 30-minute
  hardware cutoff or hardware ABI/resources.

Remaining gates: actual VK refresh/provider memory, shipping cutoff on disposable
hardware, concurrent Mihomo/services, MTU/loss/no-leak, reboot/rollback, and owner
authorization for any installation. KN-1012 is not accessed or admitted.
