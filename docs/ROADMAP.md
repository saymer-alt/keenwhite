# KeenWhite roadmap — own-code program, not a fork

Each phase needs test evidence; dates are not delivery promises.

## KW-000: Foundation / DONE locally

- Original Go module and CLI; strict provider catalog and read-only offline Doctor.
- Tests for localhost-only, invalid ports, duplicate IDs, colliding listeners, evidence states, false production status.
- CI Go vet/race and ARM64/MIPSel **build-only**; no release or router installation.
- Own README, architecture, licence, security and contribution docs.

## KW-00A: Consolidate WDTT provenance / SOURCE IMPORTED, CI PASS

- Create the real new GitHub repository, enable main protection and CI.
- Record 27 historical WDTT branches and all key outstanding draft PRs (see migration ledger).
- [x] Merged full-history GPL WDTT subtree [PR #1](https://github.com/saymer-alt/keenwhite/pull/1) into KeenWhite main; **234/234 blobs identical**, 152 upstream commits carried. Isolated WDTT Go-native/race and ARM64/MIPSel QEMU checks PASS. No hardware acceptance or release.
- Migrate future engineering discussion from KAS into KeenWhite; keep backlinks to original issues.

## KW-001: Contract alignment / DONE (code), emulator integration NEXT

- [x] Provider Contract v1 (`internal/provider`, PR #14): versioned interface Validate/Prepare/Status/Start/Stop/Health; evidence levels; PROCESS_RUNNING / LISTENER_READY / TRAFFIC_E2E_VERIFIED kept distinct; measurement TTL with stale E2E downgrade.
- [x] Hardened in task #11/#12 (PRs #22/#23/#25): admission at every boundary, plan-integrity, generations/ownership, bounded stop, LabGrant, fail-closed evidence.

- Make native KeenWhite adapter contract versioned and map it explicitly to KAS MT-001/MT-002 JSON Schemas and status freshness semantics.
- Read-only protocol/port conflict checks, offline negative fixtures, no PID or service manipulation.
- Evidence must distinguish `SOURCE_CONFIRMED`, `BUILD_CONFIRMED`, `E2E_VERIFIED`, `UNKNOWN`, `UNSUPPORTED`.

## KW-002: Consolidation and first executable provider — WDTT

- [x] Pinned WDTT PR #42 imported into `transports/wdtt/` preserving Git ancestry and GPL-3.0 (KeenWhite PR #1). Keep original repo/PRs untouched; source import does not confer live service readiness.
- Build a separate independently authored KeenWhite adapter for lab-only WDTT binary and private profile; do not fake original authorship or rewrite wire protocol.
- [x] Adapter v0 exists (`internal/provider/wdtt`, PRs #15/#16/#22/#25): full admission re-verified at Start, 1800s hard stop pinned and enforced on an injected clock, generation/ownership-verified stop (TERM→grace→KILL bounded), LabGrant-gated lab executor, secret-free diagnostics.
- [ ] TCP/UDP E2E proof and RAM measurement — requires emulator/physical acceptance (issue #3). Identity, checksum, resource admission, owner PID, fresh status are implemented and tested offline; the 30-minute owner hard stop is enforced by the adapter and documented.
- Reuse KAS PR #125 integration, never create a second watchdog; retain 512 MB combined gate, no global KAS gate.
- Do not implement before separate owner approval and hardware acceptance evidence.

## KW-003: Second independent carrier — OpenFlux

- Review upstream release tag vs main, pin source + binary, check GPL process boundary.
- Isolated exit-side/server compatibility; plan DNS behavior on router, IPv6 deny, explicit SOCKS loopback, transport selection.
- Keep direct-to-exit baseline separate from independent carrier tests (Yandex, cups.online); Mail.ru is part of VK ecosystem.
- Measure RAM/CPU/PSI and real TCP/UDP traffic on separate hardware; default no auto-start.

## KW-004: Product beta

- A single owner-confirmed provider manager interface and portable BusyBox/Entware install package built independently of KAS.
- KAS becomes OPTIONAL installer: separate release channels, deterministic rollback and ownership-aware uninstall.
- No release before physical, fail-closed and prolonged-cycle testing.

## KW-005: Advanced policy / DEFER

- Multi-provider status dashboard, authenticated control plane, manual selection and eventually conditional failover if proven safe.
- Future providers must be independently licensed, compatible and accepted; more transports is not an acceptance criterion.

## Explicit non-goals

No uncontrolled copy-paste of other upstream implementations; WDTT history-based GPL migration is an intentional, separately reviewed exception. No forced VPN monolith, no production release from CI, no replacing native Keenetic routing, no automatic modifications to DNS/iptables or working Mihomo, no assumed whitelist availability.
