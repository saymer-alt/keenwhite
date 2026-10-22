# Security policy (development stage)

KeenWhite 0.1.0-dev currently cannot open a tunnel, dial a VPS, change DNS/routes, inspect process state, or execute a transport. Any report of such behavior is a defect.

## Reporting

Open an issue **without** sensitive data. Do not attach real profiles, HWIDs, account credentials, signed links, cookies, tokens, unredacted logs, IPs/endpoints, or customer information. For sensitive vulnerabilities, use GitHub private vulnerability reporting when available, or contact the project owner privately.

## Future release gates

- Clean secret boundaries: config references only, private directories 0700 and files 0600; scrub logs/status/errors.
- No wildcard SOCKS listener, proxy exposure, arbitrary script execution, direct WAN fallback, DNS/IPv6 leakage or alternative network route without explicit user choice and E2E verification.
- Verify original binary SHA, platform ABI, ownership, process identity, and measured resource headroom before execution.
- A port in LISTEN state != connected transport; no green health without fresh TCP/UDP E2E evidence.
- No automatic trust in upstream code, dependency or release tag; pin commits, review diff, verify provenance, CI and licenses.
- Keep routing, WAN priority, Mihomo, Proxy Client, Netfilter and KAS unchanged absent explicit owner GO.
