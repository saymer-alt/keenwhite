# WDTT source migration: accepted provenance, CI and remaining hardware gates

**Updated: 2026-10-22.** Source import [KeenWhite PR #1](https://github.com/saymer-alt/keenwhite/pull/1) is **MERGED** into main at `571784f52c420ce0528dfbccbcf3b8c3effbe273`. This does **not** declare WDTT production ready.

## Import and license

- GPL-3.0 source: [WDTT Suite](https://github.com/saymer-alt/wdtt-suite) owner candidate [PR #42](https://github.com/saymer-alt/wdtt-suite/pull/42), source commit `d39f9dad91639a0f1ca321df72f3dc51ff0b0af0`.
- Original full-history subtree merge: `fd25acf17e0c5568f435d7220ffc724d26625f0e` has parents KeenWhite root `b834c8e13f8ac853130bb5c78034fed77b8857bb` **and** the exact WDTT source commit.
- Git recursive-tree comparison: **234 of 234 file blobs identical**, 0 missing, 0 changed, 0 extra; untruncated source and import trees.
- Source author's historical secrets-regex scan reported clean; keep provenance and notices. That scan is not a guarantee no secret exists in any repository history.

## Dedicated independent CI now implemented

[KeenWhite WDTT CI](https://github.com/saymer-alt/keenwhite/blob/main/.github/workflows/wdtt-import-ci.yml) merged through [PR #10](https://github.com/saymer-alt/keenwhite/pull/10) into the WDTT migration branch and then included in [PR #1](https://github.com/saymer-alt/keenwhite/pull/1).

Real passing runs:
- [Import PR #1 WDTT full CI](https://github.com/saymer-alt/keenwhite/actions/runs/39824556755) — provenance, GPL/license and 30-minute lab stop PASS; WDTT root, `go_client` and `hardened-relay` Go vet + short tests PASS; race tests on client and relay PASS; Linux ARM64 and MIPSel CGO=0 crossbuild, ELF machine assertions and selected synthetic QEMU tests PASS.
- [Import PR #1 core CLI CI](https://github.com/saymer-alt/keenwhite/actions/runs/39824556730) — PASS.

The Go runtime modules remain **nested and separate** from KeenWhite MIT root Go module. Passing Go/QEMU synthetic tests does not mean working TURN/VK sessions, verified WireGuard over carrier, ISP-whitelist availability, real UDP Associate E2E, memory budget under load or physical Keenetic ABI acceptance.

## Remaining acceptance gates

| Gate | Status | Requirement |
|---|---|---|
| Git ancestry and GPL exact source blobs | **PASS** | Preserve upstream license and authorship when editing |
| WDTT native go vet/short and race | **PASS** | Keep on future WDTT PR and main source changes |
| Crossbuild + ARM64/MIPSel selected QEMU tests | **PASS** | Hash future binaries separately; do not reuse old release hashes |
| Actual Entware SDK/ABI and ipk packaging | **NOT VERIFIED HERE** | Separate pinned SDK/QEMU/real Entware ABI test |
| Hardware TCP/UDP E2E, worker 18/1, reconnect | **NOT TESTED** | Owner-approved isolated NC-4210 acceptance with reproducible server and profile |
| KAS+WDTT admission ≥512 MB | **POLICY ONLY** | Keep device-specific RAM, RSS/CPU/PSI and MemAvailable proofs |
| Fail-closed routes, DNS/IPv6, no accidental DIRECT/WAN | **NOT TESTED** | Explicit negative carrier loss, startup and shutdown routing checks |
| Secrets and lifecycle | **PARTIAL SYNTHETIC** | End-to-end STDIN redaction, server wire/short-link, 30-min TIME_LIMIT exit 75, clean stop with test evidence |

Source repository's 27 historical branches, original Draft PRs and experimental releases remain available. **No transport installation, auto-start, release, tags, VPS operations or modification of Keenetic routing has been authorized by this source merge.**

Next: first-party WDTT adapter with pinned binary provenance, ownership-safe process lifecycle and read-only status (KeenWhite [#3](https://github.com/saymer-alt/keenwhite/issues/3)); KAS optional integration [#6](https://github.com/saymer-alt/keenwhite/issues/6) only after physical owner approval.
