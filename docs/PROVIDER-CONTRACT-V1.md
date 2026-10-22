# KeenWhite Provider Contract v1

**Status:** implemented in `internal/provider` (this PR). Offline-first; no command
starts a tunnel, edits router settings, or accesses the network.
**Version:** `keenwhite.provider/v1`, `manifest_version: 1`.

## 1. What it is

A versioned Go contract that lets KeenWhite manage transport providers
(WDTT today; OpenFlux and others later) as **separate processes**, with
explicit evidence levels, separate lifecycle states, and measurement
freshness. It re-derives the semantics of the research contracts
MT-001/MT-002 (KAS PR #175 / PR #181, tracked as KeenWhite Issue #4) in Go
code with negative tests. The KAS JSON Schemas were **not** copied; every
semantic rule below is enforced by code and a test.

## 2. Interface

```go
type Provider interface {
    Name() string
    APIVersion() string
    Validate(ctx) []Problem        // static config checks, no execution
    Prepare(ctx) (PrepareResult, error) // artifact+profile+resources+plan, no execution
    Status(ctx) (Status, error)
    Start(ctx) error               // via the configured Runner only (mock/lab)
    Stop(ctx) error                // ONLY the process this adapter started
    Health(ctx) (Health, error)    // snapshot + measurement time + TTL
}
```

| Method | Maps to MT-001 ops | Notes |
|---|---|---|
| `Validate` | install/validate gate | manifest_version, api_version, binds, evidence rules |
| `Prepare` | prepare/preflight | artifact hash+arch (reuses `internal/artifact`), profile files, resources, SOCKS port, executable `StartPlan` |
| `Status` | status | lifecycle snapshot (`Status`) |
| `Start` | start | through `Runner`; `real` executor returns `ErrForbiddenByPolicy` until hardware acceptance |
| `Stop` | stop | identity-verified signal to OUR process only |
| `Health` | health/doctor | `Health` with explicit freshness |

## 3. Vocabulary

**Evidence (how a claim is backed):** `UNKNOWN`, `UNSUPPORTED`,
`SOURCE_CONFIRMED`, `BUILD_CONFIRMED`, `E2E_VERIFIED` — same semantics as
MT-001. `E2E_VERIFIED` requires `evidence.evidence_url` **and** a
`measured_at` inside its TTL; stale measurements are hard errors.

**Lifecycle states (what is actually happening):** `IDLE`, `PREPARING`,
`PROCESS_RUNNING`, `LISTENER_READY`, `TRAFFIC_E2E_VERIFIED`, `STOPPED`,
`FAILED`, `FORBIDDEN_BY_POLICY`. `PROCESS_RUNNING` ≠ `LISTENER_READY` ≠
`TRAFFIC_E2E_VERIFIED`: a loopback listener alone is never proof of transport.

**Freshness:** `Health.TTL` (default 30 s) — expired snapshots downgrade
`E2E_VERIFIED` to `UNKNOWN` via `EffectiveTCP/EffectiveUDP`; manifests carry
`measured_at` + optional `measurement_ttl_seconds` (default 90 days) with the
same downgrade rule at validation time.

## 4. Manifest v1 (summary)

```json
{
  "manifest_version": 1,
  "api_version": "keenwhite.provider/v1",
  "provider": "wdtt",
  "artifact": {"path": "…", "sha256": "<64 hex>", "arch": "arm64|mipsle"},
  "profile":  {"path": "…", "required_files": ["…"]},
  "socks":    {"bind": "127.0.0.1:1081", "tcp": "SOURCE_CONFIRMED", "udp": "SOURCE_CONFIRMED"},
  "resources":{"min_mem_available_mb": 0, "kas_combined_min_ram_mb": 512, "admission_source": "KAS-#137-combined-admission-policy"},
  "lifecycle":{"hard_stop_seconds": 1800, "stop_grace_seconds": 10},
  "evidence": {"evidence_url": "…", "measured_at": null}
}
```

Negative rules (each has a test): unknown fields rejected; `manifest_version`
≠ 1 rejected; SHA256 must be 64 lowercase hex; arch ∈ {arm64, mipsle}; binds
must be `127.0.0.1` + port 1024–65535 (reuses `catalog.ValidateBind`, so
`localhost`, `0.0.0.0`, `127.0.0.2`, IPv6 and privileged ports fail);
`E2E_VERIFIED` without url/measured_at rejected; stale measurement claiming
E2E rejected; `hard_stop_seconds ≤ 0` rejected — a hard time limit is
mandatory (WDTT: exactly 1800).

**RAM policy boundary:** `resources.kas_combined_min_ram_mb` is
documentation-only in KeenWhite. The KAS+WDTT ≥512 MB admission policy
belongs to KAS (KAS #137) and is never applied globally to KeenWhite,
OpenFlux, or any other provider.

## 5. Process & port safety

- Transports run as **separate processes**; no third-party transport code is
  linked into the KeenWhite binary.
- `Stop()` verifies the recorded process identity (PID + exe + start time)
  before signalling; it never acts on an arbitrary PID.
- SOCKS binds are loopback-only and catalog-unique; port conflicts are a
  validation error, and an open port alone is not a transport test.

## 6. Deliberate non-goals of v1

No UDP/TCP traffic probes yet (E2E claims stay manual and referenced), no
multi-provider orchestration, no auto-restart, no watchdog — KAS remains the
installer and KeenWhite remains offline-first until the owner decides
otherwise.
