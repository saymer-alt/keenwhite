# KeenWhite Emulator Test Harness

**Status:** implemented in `internal/harness` (task #10 stage E). The harness
runs 18 lifecycle/security scenarios against INJECTED environments; today the
environment is `local-fake` (mock runner, fake clock, fake memory probe). No
scenario touches a real router, real credentials, or the network.

## 1. Purpose and future pipeline

```
GitHub Actions  →  Keenetic Emulator  →  KeenWhite Test Harness  →  PASS/FAIL
```

The harness is deliberately **not tied to one emulator**. Everything an
emulator would provide enters through the `harness.Environment` struct and
the `wdtt.Runner` / clock / memory seams the adapter already accepts:

| Environment kind | Runner | Clock | Memory | Status |
|---|---|---|---|---|
| `local-fake` | `wdtt.MockRunner` | injected step clock | injected value | implemented |
| `keenetic-emulator` (future) | runner driving the emulated router (QEMU/SDK) | emulator-provided | read from the emulated device | not built — emulator not ready |

A future emulator integration is a new Environment + Runner implementation;
`AllScenarios()` stays unchanged.

## 2. Scenario set (task #10 stage E, 18 scenarios)

| # | Scenario | What is proven |
|---|---|---|
| S01 | validate_config_success | clean config passes Validate |
| S02 | missing_binary | Prepare fails on absent artifact |
| S03 | wrong_sha256 | hash mismatch fails Prepare |
| S04 | wrong_elf_arch | arch mismatch fails Prepare |
| S05 | socks_port_conflict | occupied loopback port fails Prepare |
| S06 | insufficient_memory | provider memory admission enforced |
| S07 | missing_or_corrupt_profile | profile presence is a Validate error |
| S08 | fake_runner_start | mock start reaches PROCESS_RUNNING + alive health |
| S09 | double_start_refused | second Start refused, one process total |
| S10 | stop_only_owned_process | no-process stop is a safe no-op; foreign handles never signalled |
| S11 | process_crash | crash surfaces as FAILED + recorded exit error |
| S12 | hung_process_escalation | TERM → bounded grace → KILL, same handle |
| S13 | hard_stop_controlled_time | 30-minute WDTT lab stop fires on a fake clock (no real waiting) |
| S14 | no_secrets_in_logs | canary link/user/pass absent from diagnostics; LogScrubber redacts process output |
| S15 | transport_loss | unexpected process death → FAILED, health not alive |
| S16 | stale_health_downgrades | stale E2E health downgrades to UNKNOWN |
| S17 | unbacked_e2e_claim_rejected | E2E_VERIFIED without url/measured_at is a validation error |
| S18 | start_stop_cycles_cleanup | three full cycles; final state STOPPED; last process reaped |

## 3. Report format (report_version 1)

`harness.Run(env, AllScenarios())` returns a JSON-marshalable `Report`:
`report_version`, `tool`, `api_version`, `environment{kind,emulator,notes}`,
`started_at`, `duration_ms`, `scenarios[]{name,status,detail,duration_ms}`,
`summary{total,pass,fail,all_ok}`.

- `WriteReport(path, report)` stores it (0600) for CI artifacts.
- Reports are **secret-free by construction**: scenarios never place profile
  values into results; `TestReportIsSecretFree` proves it with canaries.
- CI: the `offline-checks` job runs the harness and uploads
  `harness-report.json` when `KW_HARNESS_REPORT` is set.

## 4. Passing / failing

A CI run of the harness is **synthetic evidence only** — it proves the
adapter logic, never device behaviour. `PASS` here is not physical
acceptance (see Issue #3), and `E2E_VERIFIED` claims still require a real,
referenced, fresh measurement.
