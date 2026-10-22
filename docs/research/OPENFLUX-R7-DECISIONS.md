# OpenFlux v0.13.0: updated decision record

**Status: SOURCE RESEARCH ONLY.** Linked work: [KW-O1 #5](https://github.com/saymer-alt/keenwhite/issues/5). No physical router test, server installation, release or proven traffic outcome.

## Source pin and release divergence

- Upstream GPL-3.0 project: [p1neappleXpress/OpenFlux](https://github.com/p1neappleXpress/OpenFlux).
- Checked 2026-10-22: upstream `main` and fork `saymer-alt/OpenFlux main` both at `edcf77a1848a72370cfba9f346d3b897b61e7e7d`.
- Tag `v0.13.0` points to `4f9c786cdd1d9fcd0c133e2e0fec0c5a12522610`, 287 commits ahead of upstream main. A workflow that only fetches upstream main with `--no-tags` misses this release. Tag upgrade must be prepared and reviewed separately, not automatically merged into main.
- Detailed source audit / ARM64 local build are recorded in [KAS Draft PR #199](https://github.com/saymer-alt/keenetic-auto-setup/pull/199); **required corrections** were submitted in its [review comment](https://github.com/saymer-alt/keenetic-auto-setup/pull/199#issuecomment-6343532855).

## Corporate independence vs transport path

- `mailru` is part of the **VK corporate ecosystem**. Do not present `direct,mailru,cupsonline` as three independent companies. MAX/OneMe is also VK-owned; CSQTT depends on VK calls.
- `direct` means TCP from OpenFlux client to our own exit (not a SOCKS DIRECT fallback to destination websites). A `direct:100` lane will be used ahead of less-prioritized carriers when available, masking their behaviour. To test third-party carriers, REMOVE `direct` and do not configure `--direct-dial`; keep the direct-to-own-exit test as a separate baseline. For the baseline, set BOTH client `--direct-dial` and exit `--direct-listen` explicitly; source `docs/sessions.md` documents this requirement.
- Yandex and cups.online are distinct candidate infrastructures for carrier-only evaluation; usable connectivity and reliability have **not** been proven on a router. They still require a compatible self-controlled OpenFlux exit.

## DNS, UDP and security gates

- OpenFlux `tunnel/tunnel.go:resolveIPv4` resolves SOCKS hostname locally at the client/router. SOCKS5 does **not** guarantee DNS-through-tunnel. Require domain vs literal-IP traffic capture and explicit Keenetic/Mihomo DNS policy before declaring no DNS leaks.
- Explicit loopback SOCKS inbound `127.0.0.1:1083` instead of default wildcard `:1080`. Verify no unexpected IPv6, WAN/direct fallback or other listening sockets.
- Both client and exit must use strict negotiated Session (`--negotiate`) with a private shared key; the documented static key exchange does not provide forward secrecy. Per-device secrets must not be committed to Git.
- `--upstream-proxy` for L4 exit can apply to TCP while UDP exits directly from the VPS host: document and test the intended exit egress/IP policy.
- Missing evidence: ARM64 hardware RSS/CPU/PSI, TCP+UDP E2E, loss/recovery, captive-service failure, correct process termination, no-bypass under carrier/exit loss, 512MB-class devices and MIPSel build/runtime.

**Recommended next work:** safe standalone KeenWhite OpenFlux adapter design (external-process + pinned artifact + loopback + provenance); offline negatives first, then separate owner-authorized lab. No GPL source copied into MIT core by this document.
