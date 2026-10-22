# KeenWhite transport source modules

The first independent in-tree source module is [WDTT](wdtt/) — GPL-3.0 source imported from [WDTT Suite](https://github.com/saymer-alt/wdtt-suite) combined experimental owner-lab PR #42 at `d39f9dad91639a0f1ca321df72f3dc51ff0b0af0`.

- Imported with complete Git ancestry via non-squash [KeenWhite PR #1](https://github.com/saymer-alt/keenwhite/pull/1), main merge `571784f52c420ce0528dfbccbcf3b8c3effbe273`.
- All 234 original file blob SHAs matched the source; original [GPL-3.0 license](wdtt/LICENSE) and authorship remain.
- Isolated [WDTT CI](../.github/workflows/wdtt-import-ci.yml) covers three nested Go modules, native/race tests and ARM64/MIPSel synthetic QEMU tests.
- **Source import and CI are not field readiness**: no owner-approved Keenetic TCP/UDP, RAM/PSI, DNS/egress or fail-closed acceptance. 30-minute owner-lab hard stop remains active. There is no stable release, startup service, or KAS installer integration.
- Root [KeenWhite CLI](../cmd/keenwhite/) is independently written MIT code; WDTT remains GPL. Never treat the root MIT license as relicensing WDTT.

References: [migration ledger](../docs/migration/WDTT-SOURCE-LEDGER.md), [acceptance matrix](../docs/migration/WDTT-CI-ACCEPTANCE.md), KeenWhite [#2](https://github.com/saymer-alt/keenwhite/issues/2) and [#3](https://github.com/saymer-alt/keenwhite/issues/3). Other upstreams remain researched providers, not automatically imported or supported.
