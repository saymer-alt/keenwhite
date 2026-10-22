# Provenance of researched sources and selected source migrations

This is a provenance ledger for future engineering. Each link is to an independent third-party or owner-maintained project. KeenWhite keeps its own root history and original core. **Exception (COMPLETED):** WDTT was imported via [KeenWhite PR #1](https://github.com/saymer-alt/keenwhite/pull/1) as GPL-3.0 source with preserved ancestry and authorship. KeenWhite itself remains a standalone GitHub repository.

| Source | Main useful idea | Provenance/licensing gate |
|---|---|---|
| [WDTT Suite](https://github.com/saymer-alt/wdtt-suite) | Local SOCKS5 TCP/UDP, TURN worker telemetry, bounded life/cleanup | Full-history GPL-3.0 source already imported to `transports/wdtt/` (pinned PR #42), preserve GPL-3.0, upstream authors, private protocol/profile and 30-min lab hard-stop |
| [YPtun v3.7.1](https://github.com/yanisplugg/yptun/tree/6d78e96469db5df2aece311cd4ab3f5458ca58a9) | Bundled qWDTT library host API, TURN credential refresh/pacing, RAW/WG+SOCKS5, WDTT Plus server as reference — [source review](research/YPTUN-WDTT-IMPLEMENTATION-20261022.md) | GPL-3.0; qWDTT code derived from SpaceNeuroX v1.4.4, server from Ivan4537 WDTT Plus v17; research only, **not** approved code import or verified router E2E |

| [OpenFlux](https://github.com/p1neappleXpress/OpenFlux) | Carrier adapters, negotiated sessions, native Linux SOCKS, exit node | GPL-3.0; remain a separate executable, do not paste code; tag v0.13.0 differs from upstream main |
| [CSQTT](https://github.com/amurcanov/csqtt) | Alternative VK transport, bounded single-binary SOCKS considerations | PolyForm Noncommercial as recorded in KAS R7; check current LICENSE, independent compatible server, no assumption of UDP or VK independence |
| [Swinter](https://github.com/somewhat-industries/swinter-ios) | MAX TURN/DTLS/WG transport architecture | Unlicense reported in R7; check vendored components and API terms, MAX is within VK ecosystem |
| [maxcalls](https://github.com/icyfalc0n/maxcalls) | MAX call signaling architecture | Inspect GPL-3.0-or-later restrictions and credentials before any implementation |
| [whitelist-bypass](https://github.com/kulikov0/whitelist-bypass) | Headless WebRTC diagnostics, lifecycle and refresh risks | Upstream licenses per file; avoid importing auth or secret-handling code |
| [WhiteTransport Public](https://github.com/meanwebuser/whitetransport-public) | Provider/carrier abstraction, evidence contracts, separation of control/data | Architecture ideas only, actual source license and artifact provenance require review |
| [KAS MultiTransport](https://github.com/saymer-alt/keenetic-auto-setup/issues/139) | Provider contract, privilege separation, lifecycle and admission | Separate project contract mapping, not a parent source tree |
| [link-generators](https://github.com/saymer-alt/link-generators) | Config/profile authoring and static validation | Separate GUI/editor, not transport execution code |

Except for the separately approved WDTT full-history import, copied third-party source requires: exact commit, SPDX/notice review, direct compatibility assessment, dependency license audit, security review, attribution and explicit owner approval. Generic ideas and independently written algorithms do not require copying source.
