# WDTT migration: review, nested CI and lab gates

Tracking: [KW-M1 #2](https://github.com/saymer-alt/keenwhite/issues/2) · [KW-M2 #3](https://github.com/saymer-alt/keenwhite/issues/3) · [WDTT migration Draft PR #1](https://github.com/saymer-alt/keenwhite/pull/1).

## Verified Git metadata

- Source repo `saymer-alt/wdtt-suite` (GPL-3.0 fork of `hoaxisr/proxy-turn-vk-android`).
- Combined candidate WDTT PR #42 source: `d39f9dad91639a0f1ca321df72f3dc51ff0b0af0` (composes PR #34 and PR #41).
- KeenWhite Draft PR #1 HEAD: `fd25acf17e0c5568f435d7220ffc724d26625f0e`; its Git-subtree merge commit parents are KeenWhite foundation `b834c8e13f8ac853130bb5c78034fed77b8857bb` AND original WDTT `d39f9dad91639a0f1ca321df72f3dc51ff0b0af0`. The original WDTT history is linked, not squashed. Original notices and `transports/wdtt/LICENSE` need final owner review before merge.
- PR #1 author reports a full-history regex scan and successful source tests. Repeat sensitive-path/secret checks before deciding to merge; scanning cannot prove absence of every secret.

## Critical CI gap

KeenWhite current [root CI](https://github.com/saymer-alt/keenwhite/blob/main/.github/workflows/ci.yml) runs `go test -race ./...`, `go vet ./...`, and ARM64/MIPSel builds for **the KeenWhite CLI module only**. WDTT is nested with its own Go modules (root `transports/wdtt/go.mod`, `transports/wdtt/go_client/go.mod`, and `transports/wdtt/hardened-relay/go.mod`); these are not validated by the root `go test ./...`.

Therefore a green KeenWhite CI result on Draft PR #1 cannot be used as a claim that the imported WDTT client compiles or works on a Keenetic.

| Gate | Next action | Current status |
|---|---|---|
| GPL and authorship | Verify original `LICENSE`, notices and Git ancestry; no source rewriting | Import reported / independent review pending |
| Confidentiality | Repeat targeted Git-tree/history scan, no raw logs/secrets printed | Import reported / independent review pending |
| Nested Go modules | Dedicated CI for WDTT root, client, relay (`go test`, `go vet`, bounded-race where feasible) | NOT COVERED BY KEENWHITE ROOT CI |
| ARM64/MIPSel binary | Separate transport builds, ABI/ELF/provenance + SHA; do not publish automatically | NOT COVERED BY KEENWHITE ROOT CI |
| Protocol and lifecycle | Verify 18/1 vs 18/2, SOCKS TCP+UDP, teardown, bounded pipe, redaction of TURN creds, short-password links | NEEDS PR/DEVICE-SPECIFIC TESTS |
| Owner-lab restrictions | Preserve 30-minute TIME_LIMIT / exit 75 hard-stop, no auto-start, private profiles | MUST REMAIN ENFORCED |
| Real hardware | Owner-approved NC-4210 baseline; separately measure KN-1012/MIPSel and memory/RSS; fail-closed/egress | NOT TESTED IN KEENWHITE |

Never transfer the KAS+WDTT combined ≥512MB policy to generic KeenWhite core or OpenFlux without evidence. Never confuse a SOCKS listener with verified exit connectivity. No changes to router routes, DNS, ProxyN, Mihomo, VPS, or production networks in this migration.

The WDTT source repository's 27 research branches, old PRs, releases and published binaries stay intact until the owner explicitly accepts a validated cutover. WDTT's GPL code remains GPL in the KeenWhite subtree even though the original KeenWhite core is MIT.
