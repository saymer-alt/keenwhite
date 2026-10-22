# YPtun 3.7.1: reference audit for KeenWhite / WDTT

**Reviewed:** 2026-10-22 · **Status:** `SOURCE_REVIEW_ONLY` · **No code import, no deployment, no E2E acceptance.**

This note preserves a **source-pinned engineering reference**, not a proposal to fork YPtun or merge its bundled dependencies. KeenWhite is the product owner; its initial `main` only implements offline catalog/Doctor. The history-preserving WDTT Suite import remains gated in [KeenWhite Draft PR #1](../../pull/1).

## Exact provenance / evidence

- YPtun source: [yanisplugg/yptun](https://github.com/yanisplugg/yptun), **v3.7.1**, [commit `6d78e96469db5df2aece311cd4ab3f5458ca58a9`](https://github.com/yanisplugg/yptun/tree/6d78e96469db5df2aece311cd4ab3f5458ca58a9) (2026-10-21 release).
- YPtun `wdtt/` was adapted from **SpaceNeuroX/proxy-turn-vk-android**, qWDTT v1.4.4 commit `a296c57`; see [THIRD_PARTY_NOTICES](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt/THIRD_PARTY_NOTICES.md).
- YPtun `wdtt-server/` claims provenance from **Ivan4537/WDTT-Plus**, release v17 commit `abf0a0f`; see [server README](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt-server/WDTT-SERVER-README.md). Client and server sources have **distinct ancestry**.
- Earlier owner-maintained review: [WDTT Suite issue #43](https://github.com/saymer-alt/wdtt-suite/issues/43). Our existing acceptance/security track: [KeenWhite #3](https://github.com/saymer-alt/keenwhite/issues/3); previous [WDTT Suite #25](https://github.com/saymer-alt/wdtt-suite/issues/25), [#29](https://github.com/saymer-alt/wdtt-suite/issues/29), [#35](https://github.com/saymer-alt/wdtt-suite/issues/35), [#40](https://github.com/saymer-alt/wdtt-suite/issues/40).
- Licenses: YPtun itself GPL-3.0; `wdtt/` and `wdtt-server/` declared GPL-3.0. KeenWhite original core is MIT; importing or combining third-party code is **not** authorized by this note. Retain notices, authorship, upstream ancestry, and review dependencies if code is ever incorporated.

### Where the relevant YPtun code lives

| Source | Observation from inspected code |
|---|---|
| [`wdtt/main.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt/main.go) | `Config`, `Run(ctx, cfg)`, `OnConfig`, VK/CAPTCHA hooks, pause/running state; WG vs RAW/TURN-TCP/no-DTLS modes |
| [`wdtt/group.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt/group.go) | Nine-worker groups, TURN-credential stream isolation, refresh serialization, pacing of allocations |
| [`wdtt/session.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt/session.go) | TURN/DTLS session lifecycle candidate for a targeted **diff**, not yet a full line-by-line review |
| [`wdtt/raw_socks.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt/raw_socks.go) | RAW userspace netstack, loopback SOCKS5 TCP CONNECT and UDP ASSOCIATE |
| [`wdtt/socks_wg.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt/socks_wg.go) | WireGuard config parser and userspace netstack adapter |
| [`wdtt-server/main.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/wdtt-server/main.go) | Standalone server flags for DTLS, direct, RAW, local WG, authentication, state and admin API |
| [`cores/coreapi/coreapi.go`](https://github.com/yanisplugg/yptun/blob/6d78e96469db5df2aece311cd4ab3f5458ca58a9/cores/coreapi/coreapi.go) | Unified host API; broad mobile/desktop integration is **not** a Keenetic binary design requirement |

## Findings and transfer decisions

| Finding | Why it matters on Keenetic | Decision |
|---|---|---|
| Host-neutral lifecycle `Run(context.Context, Config)`, `OnConfig`, `SetCaptchaRequestHook`, `PushCaptchaResult`, `SetVKAuthRequestHook`, `PushTurnCreds` | A controlled Go interface may avoid parsing secret-bearing STDIN/stdout while enabling optional CLI compatibility | **TAKE IDEA**, design own narrow adapter; first compare PR #42/our existing hooks |
| Distinct `RawMode`, `RawTunFromHost`, `TurnTCP`, `NoDTLS` | Explicit capabilities prevent a client choosing unsupported server wire/transport combinations | **TAKE IDEA** for versioned capabilities and fail-closed validation; **do not assume** arbitrary WDTT public server supports these modes |
| Nine workers per anonymous group; `credStreamID=groupID*100`; per-group allocate pacing 100 ms and refresh gate 15 s | Helps investigate rate limits/486 and 9/1 vs 18/1 vs 18/2 field interoperability | **COMPARE FIRST** with [KeenWhite #3](https://github.com/saymer-alt/keenwhite/issues/3) / our pinned PR #42; never silently alter worker count or auth semantics |
| RAW `SOCKS5 CONNECT` + `UDP ASSOCIATE` using userspace `netstack` | Candidate reference for router-local SOCKS and independent UDP evidence | **RESEARCH ONLY**: no verified equivalent real E2E and resource safety |
| `raw_socks.go`: `conns := make(map[string]net.Conn)` by UDP target, closed at control-session exit, no obvious per-target eviction/bound in reviewed path | Possible unbounded target churn, FD/goroutine/RSS growth under long-lived control TCP | **DO NOT COPY AS-IS**; add bounded sessions, cancellation, idle expiry and stress tests to any separately designed implementation |
| In `group.go`, log prints first 8 characters of VK hash plus TURN URLs | Partial secret disclosure/privacy risk in normal logs | **REJECT LOG FORMAT**; strict redaction throughout logs, argv, errors and CI. Track against existing privacy work [WDTT #40](https://github.com/saymer-alt/wdtt-suite/issues/40) and newer PR #41 |
| Default `GoDNS` in `Run` is `yandex`; peer resolved with `net.ResolveUDPAddr` before tunnel | DNS and fallback path can have different semantics from host traffic | **TEST REQUIRED** on device; check IPv4/IPv6, resolver failure, pre-tunnel queries, binding to selected interface, no unintended DIRECT/WAN fallback |
| Bundled WDTT Plus server/root SSH installer alters forwarding, NAT/firewall and BBR per documentation | Dangerous on active VPS shared with Xray/Mihomo/AWG | **ISOLATED LAB ONLY** with separate owner approval, snapshot, port matrix, rollback and provenance review |
| YPtun also includes `csqtt`, `free-turn-proxy`, `openflux`, `masterdns`, `olcrtc`, `snolc` | Useful as an ecosystem catalog for alternative carriers or DNS fallback | **REFERENCE ONLY**; CSQTT/OpenFlux are different wire protocols, not drop-in WDTT servers. OpenFlux stays under [KeenWhite #5](https://github.com/saymer-alt/keenwhite/issues/5) |

## Compatibility matrix to investigate, not a pass report

| Axis | YPtun inspected behavior | KeenWhite owner acceptance gate |
|---|---|---|
| Invocation | Library `Run(ctx,cfg)` and callbacks | Reconcile with existing WDTT CLI and preserve real 30-minute hard-stop exit **75** |
| 9/1, 18/1, 18/2 | Nine-worker groups, hash index modulo hashes | Preserve explicit worker count and distinguish syntactic parsing, TURN/DTLS registration, GETCONF, new TCP egress, UDP egress |
| Auth mode | Anonymous and account; account workers capped at four in inspected `main.go` | Validate account quota/consent, no secrets in logs |
| Transport choice | WG, RAW, optional TURN-TCP and no-DTLS direct | Server protocol negotiation and negative mode-mismatch fixtures, no automatic downgrade |
| SOCKS5 | RAW own handler TCP+UDP; WG host/netstack integration | Loopback only; correct UDP control TCP lifetime, slot cleanup, dest TTL/max, TCP half-close |
| DNS/IPv6 | Host-dependent resolver/IPv4-only paths | Test DNS leakage and IPv6 policy with router policy, interface pinning, fail closed |
| Resource model | YPtun optimized for phone/desktop, not verified small Entware targets | ARM64 NC-4210 then separate 512 MB and MIPSel; peak RSS/FD/PSI/GC/CPU, workload matched |
| Readiness | Library running/listener states | Only fresh independently measured TCP+UDP E2E; `LISTEN != E2E` |

**Important:** from earlier owner field evidence, our baseline is not a blank slate. The WDTT Suite experimental code already has bounded packet-pipe work, hard-stop tests, memory admission and diagnostics. Do **not** duplicate or regress this functionality; source diff against the exact historically imported PR #42, not only default branch or PR #34.

## Research checklist (new development only in KeenWhite after cutover)

- [ ] First compare the **pinned WDTT Suite PR #42** (source in gated migration PR #1) against YPtun v3.7.1 source: host API, cancellation/lifecycle, credentials, worker-group pacing, refresh/486, UDP churn, logs and wire modes. Categorize `ALREADY_PRESENT / TAKE_IDEA / REJECT / UNKNOWN` with exact lines.
- [ ] For **9/1**, **18/1**, **18/2**, fixture-test missing/duplicate hashes, invalid WG and RAW combinations, wrong server mode, insufficient memory and negative authentication cases.
- [ ] Test connection cleanup on TCP idle/half-close and UDP ASSOCIATE: per-destination limits, stopped control connection, timeout, repeated start/stop, 486 refresh storm, no goroutine/FD retention and no secret leakage.
- [ ] Preserve the **30-minute owner-lab hard exit 75**, 512 MB KAS+WDTT combined gate, existing per-mode RAM floors, local-only listeners, owner PID and evidence freshness. No production deployment or background watchdog.
- [ ] When ready, evaluate a dedicated disposable WDTT Plus-compatible server on a **separate authorized VPS**: no production NAT/firewall changes, confirm actual client-server protocol/wire compatibility before performance work.
- [ ] Submit any selected implementation in **small separate Draft PRs** with tests, security/licence review and provenance. Merge, new binaries/releases and physical deployment need distinct owner approvals.

## Project boundaries

- **KeenWhite:** owns the independent runtime, transport adapters, documentation, future releases and acceptance; YPtun is just a cited upstream reference.
- **WDTT Suite:** provenance and historical owner-lab findings remain accessible at [issue #43](https://github.com/saymer-alt/wdtt-suite/issues/43) and existing PR/release URLs during the gated migration.
- **KAS:** optional installer/supervisor integration only; no second watchdog and no changes to working router policies.
- **link-generators:** subscription/HWID/URI import/export concepts may inspire Config Studio separately, but are not automatically part of KeenWhite's runtime scope.

**Evidence limits:** read-only GitHub source/README inspection; no checkout, build, static-analysis pipeline, hardware measurements or packet-level interoperability validation performed for this study.
