# WDTT low-memory owner lab — Issue #19

Status: **RESEARCH ONLY; no 512 MiB hardware admission**. Allocation reductions
are demonstrated. The full local transport's peak RSS has **not** improved in
the short paired matrix. Keep every existing memory floor. No router, KAS,
Mihomo, production route, release or main/stable change is authorized here.

## Source and reproducibility

- Branch `feature/wdtt-lowmem-owner-20261013`, based on exact
  `dc78576bcc7593ad52b30334bd46670571753be1` (Draft #15 over #13).
  PR targets `feature/wdtt-tcp-idle-teardown-owner-20261012`; neither dependency
  PR is edited or merged. Candidate source is the PR head SHA, not a release.
- Toolchain Go 1.26.1 from the existing Entware SDK archive, SHA256
  `d51f30c126425ebff203b107a1a0aa9d3a5844f6c481bc6ff4c117cadc208674`.
  CGO=0, Linux amd64 WSL Ubuntu, Intel Xeon E3-1270 V2, two CPUs via taskset,
  GOMAXPROCS=2, default GOGC/GOMEMLIMIT. A test executable is not a shipping ELF.
- Three baseline/candidate pairs, alternating order, identical payload counts
  and harness. Baseline production code is archived from the SHA above; only
  common test harnesses are added. The baseline's two benchmark adapters call
  its original allocating functions. No baseline memory guard is weakened.
- `tools/check_lowmem_lab.py` independently reproduces native component and
  short full-session measurements. `--sessions` needs root in a disposable
  Linux VM/CI for a new network namespace, never on a router. Only that new
  namespace's loopback interface is enabled. No host routes/firewall changes.
- Numeric evidence is in `provenance/lowmem/`. Sampling every 100 ms: VmRSS,
  cumulative VmHWM, VmSwap, host MemAvailable, memory/IO PSI, Go HeapAlloc,
  HeapInuse/HeapSys, StackInuse, TotalAlloc, NumGC, goroutines, descriptors,
  cgroup memory.current where available. Missing kernel metrics read as zero,
  **not proof of absent pressure**. Host/cgroup values include other processes;
  they cannot establish memory made available by WDTT alone. VSZ is not RSS.
- Synthetic-only allocation profiles are private 0600 within 0700 directories.
  No pprof server or profiling option exists in the shipping executable.
  CI exports only numeric JSON/JSONL/benchmarks, never profiles, configs, IPK
  or executable artifacts. Normal tests skip the opt-in memory harnesses.
- `lowmem/measured-runtime.json` binds the measured candidate runtime files by
  SHA256 and confirms equality to the PR after canonical gofmt formatting.
  Local test ELF fingerprints are precommit test-executable evidence, not
  shipping-client checksums; CI independently builds from its exact source head.

Reproduction with the pinned SDK's `go` executable and a fresh work directory:

```sh
export GOENV=off GOTOOLCHAIN=local GOWORK=off GOTELEMETRY=off
export GOPATH=/absolute/private/module-cache GOCACHE=/absolute/private/go-cache
# Disposable Linux CI/VM only; script enforces two CPUs for WG NumCPU workers.
sudo --preserve-env=GOPATH,GOCACHE python3 tools/check_lowmem_lab.py \
  --go /absolute/sdk/staging_dir/host/go/bin/go \
  --work /absolute/private/new-lowmem-lab \
  --report-dir /absolute/new-numeric-evidence --sessions
```

## Ownership audit and unchanged limits

| Component | Budget / ownership | Consequence |
|---|---|---|
| qWDTT workers | 9, 16=9+7, 18=9+9; global account budget 1–4; hash selection unchanged | No substitution of two workers for a two-hash 18-worker profile |
| Dispatcher | SendCh=128 packets/worker; pooled packet capacity 2048 B; ReturnCh=384 | Fully occupied payloads: 256 KiB/worker (9=2.25 MiB, 18=4.5 MiB), return queue 0.75 MiB; channel metadata and pool retention extra; capacities are not eager allocations |
| RunSession | Four read arrays total 6880 B/worker; DTLS, TURN state, connutil packet pipes and goroutines extra | One live transport per worker; reconnect overlaps/loss are not bounded by this arithmetic |
| TURN socket | Requests RX and TX 625 KiB each per session, plus local dispatcher socket | Kernel may cap/double the requests; skbuff usage is lazy, outside Go heap and not equal to process RSS. Model harness records actual getsockopt values |
| WireGuard | Encryption/decryption/handshake workers each scale with runtime.NumCPU; queue sizes 1024; dynamic buffer pools, max segment 65535 B | GOMAXPROCS alone does not normalize worker count; taskset does. Burst retention can dominate tiny transport buffers |
| gVisor TCP | Default send **and** receive limits 1 MiB/endpoint, max 4 MiB, receive moderation enabled | Logical bounds, not eager 2 MiB allocations. Traffic-dependent socket/payload memory can dominate. No dependency patch or unsafe access to private stack internals |
| gVisor UDP | Default 32 KiB send/receive, max 4 MiB | Actual traffic/socket lifetime determines footprint |
| SOCKS | General 32 slots; ARM64 64 only with >=262144 KiB available at listener start; UDP controls share those slots; max 4 associations, 32 peers/association, payload 2048, frame 2304 | All caps retained. 28 TCP + 4 UDP controls fit the 32-slot test; this is not a proof of 64 saturated flows on 512 MiB |
| TCP relay | Two directional copy goroutines plus inactivity observer per connection; cancellation owns both sockets; FIN remains half-close | Progress means completed writes; 180 s inactivity timeout and cleanup retained |
| DNS / credentials | SOCKS names resolve through WG netstack; VK fhttp/TLS credential clients and their caches are a separate path | No host DNS/direct fallback added. VK credentials, captcha/images and refresh memory are **not measured** by the supplied-credential fixture |
| Obfuscation | AEAD cache retains profile keys for process lifetime; nonce/key lookup still allocate | Normal single private profile is not a per-packet key leak; no global reusable ciphertext pool introduced |

Source audit uses the locked module versions (gVisor
`v0.0.0-20250503011706-39ed1f5ac29c`, existing WireGuard/Pion locks). Kernel
socket memory, TCP TIME_WAIT, lazy pools and GC explain why idle snapshots and
sum-of-constants estimates cannot predict the traffic peak. A plateau or
post-GC reclamation in a local test does not prove all reconnect paths leak-free.

## Narrow changes and byte/lifecycle proof

1. Outgoing RunSession owns a WRAP scratch buffer until Pion's synchronous
   WriteTo returns. Pion TURN copies the payload into STUN/ChannelData before
   that write returns. AEAD, HKDF, RTP, padding, counter, nonce and wire bytes
   are unchanged. The original allocating API remains available to other paths.
2. Each UDP reply receiver owns one reusable RFC1928 frame; synchronous
   WriteToUDP consumes it. Header bytes are explicitly cleared before reuse.
   No cross-association pool, larger datagram or peer authorization change.
3. Hide the source TCPConn.WriterTo from io.CopyBuffer so the existing 16 KiB
   buffer is actually used. Go's TCP fallback otherwise calls io.Copy with an
   additional default 32 KiB buffer; there is no splice shortcut through the
   progress writer. Preserve progress, cancellation, errors and TCP FIN.
4. Centralize existing MemAvailable floors and check them at both import and
   launch. Close the missing runtime nine-worker/account check. Legacy request
   12 is checked as nine effective workers. Unknown/invalid metadata fails
   closed before sockets. Fixed secret-free error and exit 78 remain.

Frozen WRAP fixture hashes are generated from the original pinned source,
not from the new wrapper; boundary sizes, sequence wrap, tampering and 18
concurrent independent scratch buffers are tested. UDP byte/header/oversize
tests require zero steady-state allocations. Tests also exercise WriterTo
suppression, cancellation, idle activity and delayed TCP half-close.

## Before / after measurements

### Component harness (three alternating pairs)

Includes a real idle userspace WG device, saturated dispatcher queue/socket
**models** for 9/18, held TCP relays, 500000 WRAP packets of 1400 B, and 500000
UDP replies with four scratch slots. Queue/socket models are **not TURN sessions**;
held TCP remotes use net.Pipe and are not a loaded WAN benchmark.

| Metric | Baseline | Candidate |
|---|---:|---:|
| Whole-process HWM KiB, three runs | 50188 / 52728 / 50456 | 36292 / 36844 / 37120 |
| Median HWM KiB | 50456 | 36844 (−27.0%) |
| WRAP allocations / packet | 1584 B, 3 allocs | 48 B, 2 allocs |
| UDP reply allocations / packet | 1536 B, 1 alloc | 0 B, 0 allocs |
| Idle WG sampled RSS KiB | 14664 / 14808 / 14784 | 14764 / 15164 / 14760 |
| Held TCP 32 sampled RSS KiB | 22284 / 22192 / 22188 | 22020 / 22284 / 22100 |

WRAP benchmark ns/op baseline 2516/2512/2623; candidate 2024/2749/2071.
One candidate sample is slower: do not claim a universal CPU/throughput win.
UDP encoder ns/op baseline 501.4/496.3/505.1; candidate 51.66/55.32/52.46.
HWM is cumulative for the process, not a per-phase instantaneous measurement.
Final goroutines return to two; RSS/HeapSys retention is not itself a leak.

### Full local transport (three alternating pairs per worker plan)

Actual 9/18 simultaneous RunSession TURN/DTLS sessions, hardened AUTH fixture,
userspace WG/netstack and SOCKS. Explicit supplied synthetic credentials bypass
only the **test harness's** VK provider, never a shipping admission gate.
Each run verifies 84 MiB bulk data (14 upload + 14 download flows per round,
three rounds), plus 1.68 MB UDP echo payload through four associations. All
endpoints are synthetic and inside a fresh loopback-only network namespace.

| Metric | Baseline | Candidate |
|---|---:|---:|
| 9-worker HWM KiB | 121624 / 115184 / 116912 | 121744 / 121836 / 122960 |
| 9-worker median HWM KiB | 116912 | 121836 (+4.2%) |
| 18-worker HWM KiB | 128700 / 121500 / 118172 | 112812 / 125564 / 133820 |
| 18-worker median HWM KiB | 121500 | 125564 (+3.3%) |
| 9-worker idle sampled RSS KiB | 30248 / 20136 / 21536 | 28668 / 21484 / 21724 |
| 18-worker idle sampled RSS KiB | 26336 / 24528 / 28880 | 28752 / 32320 / 24888 |
| 18-worker median TCP flow seconds, per run | 2.337 / 2.443 / 2.265 | 2.397 / 2.455 / 2.440 |

All twelve short cases pass payload verification and release their owned
sessions on stop. **The component RSS reduction does not establish an E2E peak
reduction.** Less allocation can defer GC while netstack traffic allocations
remain live; this is a hypothesis, not a measured root-cause attribution.
No MemAvailable safety margin is reduced on these results.
Those twelve runs used the initial immediate-burst harness. First CI run
`38393947532` passed tests/race/QEMU/observer checks but failed on candidate
18-workers repetition 2, third burst, with ConnectionResetError. It is kept
as a failed result, not silently retried. Existing Entware acceptance run
`38393947466` passed. The updated bounded-load harness now samples actual
active SOCKS slots in a **test-only** private file and waits (maximum 10 s)
for previous TCP slots to drain to the four held UDP controls before the next
28-flow burst. This avoids inadvertently exceeding the unchanged 32-slot cap;
it does not increase the cap, retry failed transfers or prove the old reset's
root cause. Drain waits and active counts are numeric evidence. Hardware
application churn above the cap remains an explicit limitation.

Separate one-pair allocation-profile runs (excluded from the RSS table) sampled
1053.92 MiB total allocations before versus 935.72 MiB after. Baseline WRAP
accounted for about 54.57 MiB flat; it is absent from the candidate's top nodes
above 4.68 MiB. gVisor buffer allocation accounted for about 200.17/174.33 MiB;
DTLS ApplicationData.Marshal 104.14/107.14 MiB. Those sampled totals are
traffic/GC-dependent and not a repeatable peak-RSS improvement claim.
In-use profiles at cleanup attributed about 28/27 MiB to WireGuard message
buffer pools. Such profiles reflect the runtime's sampled heap/recent GC,
**not instantaneous peak or fully reclaimed post-GC memory**. This supports
further investigation of dependency/queue retention, without changing it here.

### 30-minute local soak

Paired 18-worker soaks are being measured separately from the short matrix:
disjoint two-CPU affinities, separate network namespaces, same traffic burst
each minute, four UDP controls. They include startup, load, idle between bursts
and final cleanup. Numeric final results will be recorded before final handoff.
The test harness has a fixed 30-minute context; **this does not execute the
shipping main's os.Exit(75)**. That literal production cutoff remains unchanged
and source-guarded; a timed run of the actual main on approved hardware is
still a separate acceptance requirement. No automatic restart is introduced.

The first soak attempt is excluded: both fixtures ended at the inherited
three-minute server test limit after three successful traffic bursts. Extending
the synthetic credential lifetime alone did not extend that test process.
The memory-only server run limit is now 31 minutes; default fixture behavior
and production server code are unchanged. One separate profiling attempt
failed session readiness; a retry passed. No failed run is counted as a pass.

## Memory admission and owner decision

| Profile | Effective workers | Unchanged minimum MemAvailable KiB |
|---|---:|---:|
| Account | 1 / 2 / 3 / 4 | 98304 / 114688 / 131072 / 147456 |
| Anonymous single hash | 9 | 131072 |
| Anonymous partial second group | 16 | 147456 |
| Anonymous two hashes | 18 | 155648 |

These inherited conservative floors are **not newly validated peak bounds**.
Import/runtime min−1/min tests enforce equality; unavailable RAM and invalid
metadata deny launch. The shell advice `MemAvailable >=85000` is wrong for
these profiles and must not be used. This task updates instructions here,
not KAS preflight or production integration.
The existing `qMemory()` preference for MemAvailable is retained; kernels
without that field use the existing conservative MemFree fallback, never swap
capacity or a hardcoded router-model allowance. Unreadable/unknown memory
returns zero and is denied. Neither floor is a continuously enforced runtime
reservation: after admission the owner must still watch concurrent workloads.

Owner's office KN-1012 SE: MemTotal 497676 KiB, Mihomo RSS about 112176 KiB,
stable MemAvailable 123992 KiB after owner removal of Netdata. Nine workers
remain short by 7080 KiB; eighteen by 31656 KiB. Agent does not stop services,
change the threshold, launch WDTT or treat zero PSI as extra RAM. Physical
512 MiB alone is neither permission nor a mandatory failure; MemAvailable,
traffic peaks and concurrent services matter. There is no new 1 GiB rule.

256 MiB MIPSel KN-1010 remains a separate owner-only unsupported combined
deployment experiment. QEMU validates code/softfloat ELF behavior, **not** RAM,
Entware kernel behavior or capacity beside Mihomo. Swap does not lower these
floors or turn them into a physical-RAM guarantee. Existing external-swap
admission decisions from other projects are not changed here.

## Tests, CI and remaining acceptance gates

- Full existing client short tests, race (native Linux with host gcc), new
  admission/wire/allocation tests; inherited #15 idle/cancel/half-close tests.
- Real loopback SOCKS regression: four UDP associations, fifth refused,
  60 successful association lifetimes, reclaimed slots/descriptors/goroutines,
  three listener stop/start cycles. No remote packet required.
  Status writes are disabled in that socket-ownership unit test: its first
  version exited during concurrent status updates. Existing labWrite validates
  the old inode before atomic replacement; concurrent writers can invalidate
  that inode between checks. This is an independent fail-closed diagnostic
  hazard hypothesis from source inspection; that failure was not instrumented
  to prove its exact cause. It is not relaxed/fixed here or treated as evidence
  of a transport memory leak.
  Existing status/telemetry tests and the real-session fixture remain enabled.
- ARM64 and MIPSel softfloat test executables run new tests under QEMU;
  source-only CI also retains existing independent ELF/IPK sandbox rebuild,
  opkg/lifecycle/local-pair, no-fallback and publication-boundary checks.
- Pinned observer #17 `ce2de92b91f692446a149eb1eb0880d0d2379f5a` consumer tests
  run independently; v2 telemetry producer files are byte-unchanged. Observer
  code is not merged into this branch. No Proxy1 status is treated as data proof.
- CI results are the exact PR head checks. No binary artifacts or release are
  produced by the added workflow. No new license audit or changed module locks.
- Unverified: real ARM64/MIPSel traffic RSS/CPU, anonymous VK provider/refresh,
  packet loss/reordering and slow peers, worst-case 64-flow load, shared-device
  pressure with Mihomo, actual main cutoff and subsequent owner-controlled
  recovery. No hardware acceptance or deployable package is claimed.

## Future owner-only hardware procedure (requires separate authorization)

1. First collect read-only baseline for 5–10 minutes while existing services
   remain active. Do not print configs, argv, TURN endpoints, keys or profile
   names. Confirm architecture, PID identity, available storage, temperature
   and that the selected laboratory path is isolated/fail-closed. Do not SSH
   to the working office SE as part of this PR.
2. Compare the chosen profile against the table **both at import and launch**.
   If below its floor, stop. Record min available under normal concurrent
   load; do not obtain headroom by disabling Mihomo or changing routing.
3. After an explicit owner approval and SHA-verified candidate/rollback pair,
   manually start the existing private config with the normal launcher, no
   autostart, watchdog or recovery loop. No new install/start command is
   granted by this research PR. A smaller hash/worker profile needs a genuine
   compatible owner profile, not an edited DE18 link.
4. Record before/startup/idle/load/5/10/20/29min and after-stop numeric metrics.
   Use explicitly owner-selected SOCKS-only TCP, UDP and DNS targets; no direct
   fallback and no global default route. Check handshake **and transferred
   bytes**, delayed TCP FIN, UDP churn and fourth/fifth association behavior.
5. Stop if MemAvailable crosses the admitted floor, memory/IO pressure or
   swap-in/out grows persistently, OOM occurs, unrelated services degrade,
   or the owner probe fails. Do not tune the threshold during a failed run.
6. Observe actual main TIME_LIMIT and exit **75** at 30 min, closed local
   listener and failed SOCKS data probe with Proxy1 still potentially up.
   No process should start itself again. Save numeric before/after evidence,
   then use the already approved manual rollback if needed. Repeat only after
   explicit owner decision; a monitor's old status is not recovery proof.

Read-only commands; set `WDTT_PID` to the verified numeric PID (no ps argv):

```sh
uname -m
awk '/^(MemTotal|MemAvailable|SwapTotal|SwapFree):/ {print}' /proc/meminfo
awk '/^(pswpin|pswpout|oom_kill):? / {print}' /proc/vmstat
for pressure in memory io; do
  [ ! -r "/proc/pressure/$pressure" ] || cat "/proc/pressure/$pressure"
done
# Manually enter a verified PID; rejects empty/non-numeric values.
WDTT_PID=12345
case "$WDTT_PID" in ''|*[!0-9]*) exit 2;; esac
test -r "/proc/$WDTT_PID/status" || exit 3
awk '/^(Name|State|VmRSS|VmHWM|VmSwap|Threads):/ {print}' "/proc/$WDTT_PID/status"
find "/proc/$WDTT_PID/fd" -mindepth 1 -maxdepth 1 2>/dev/null | wc -l
```

Repeat numeric sampling at one-second intervals into an owner-selected private
directory if approved. Monitor identity/start time to avoid PID reuse; do not
cat `/proc/PID/cmdline`, environment, private config or heap dumps. Go heap/
goroutine allocation profiles in this PR are a test-binary facility only;
shipping telemetry is unchanged and does not expose those metrics.
