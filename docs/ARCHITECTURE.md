# Architecture decision KW-001 — Independent KeenWhite runtime

**Status:** proposed / partially implemented (offline catalog), 2026-10-22.

## Product boundaries

```text
                      user / optional Config Studio
                                |
                                v
                       KeenWhite CLI/API
             (manifest, evidence, readiness, doctor)
                                |
                +---------------+----------------+
                |                                |
          Provider: WDTT                    Provider: OpenFlux
     OWN GPL module (planned)              OWN adapter (planned)
        separate process                    separate process
         own profile/wire                   own profile/wire
                |                                |
           own compatible                    own compatible
            exit server                       exit server

    KAS (optional installer; reuses existing supervisor)
                 installs and updates KeenWhite
```

KAS remains the optional installer and the only supervisor integration for the KAS-managed mode. KeenWhite must **not** introduce a competing watchdog. Ownership of product/source does not require linking every GPL transport into the MIT CLI; independently licensed source modules with separate process boundaries remain valid inside one repository. Standalone manual mode is a future design decision, not implemented.

## Provider identity

Each provider declares: unique ID, external upstream source, exact server-wire family, compatible server class, a prospective local loopback endpoint, separate TCP/UDP evidence levels, status of the adapter, a resource admission policy with provenance, and an evidence reference. No secret profile, credential or private URI is part of the catalog.

`wire` names are *labels*, not a formal compatibility guarantee. Real provider manifests must later map to the established KAS MT-001 contract. Do not silently equate distinct WDTT, CSQTT, OpenFlux or MAX server protocols.

## Runtime design (NOT implemented yet)

Planned API: `install/diagnose/start/stop/status/update/rollback/uninstall`, via provider-owned adapters. The manager owns only its own processes and never kills a foreign process based on a port number alone. Separate registered metadata tracks PID, executable identity, start time, artifact SHA, status freshness, listener owner, actual E2E TCP/UDP proof, and read-only resource observations.

`READY` requires independent, recent, verified data-path evidence; listener checks are neither readiness nor proof of no leaks. Safe failure of a transport must not silently switch a client policy to DIRECT/WAN. Any automatic failover policy requires a separate acceptance gate.

No resident daemon in the initial architecture. On-demand status collection and strict separation of control and data planes are the defaults.

## Security and operational boundaries

- Loopback only (`127.0.0.1:port`). No wildcard/LAN listener without later explicit decision.
- Transport config: directory 0700 and private files 0600; no shell history, telemetry, logs or CLI argv secrets.
- Physical rollout requires exact binary provenance, device ABI, memory/PSI, IP/DNS/IPv6 policy, clean install/uninstall/rollback, negative cases, and signed-off real hardware testing.
- `direct` carrier to one's own exit (OpenFlux) is distinct from an unintended direct-Internet fallback. A direct carrier at the highest priority can mask evaluation of third-party carriers.
- Third-party licensing: original KeenWhite code stays independently maintained. WDTT GPL source will be imported with Git history, license and attribution as an intentionally supported module. No unreviewed GPL/PolyForm source pasted into MIT code; adapters communicate through process/CLI/loopback interfaces.

## Stage 0 implementation

The `keenwhite` CLI in this repository only validates *non-secret* static JSON provider catalogs and reports that process, listener and egress were NOT_TESTED. No SSH, WAN, TURN, WebRTC, TUN, routes, process execution, daemon, package manager or host state change is performed.
