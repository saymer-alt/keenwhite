# Transport modules (planned)

KeenWhite starts as an original first-party manager. The first in-tree transport module will be **WDTT** under `transports/wdtt/` after a controlled, *non-squashed Git subtree import* from the pinned WDTT Suite owner-lab PR #42 commit. On `main` there is **no WDTT source yet**; the import is prepared and gated in Draft [PR #1](https://github.com/saymer-alt/keenwhite/pull/1) (pinned `d39f9dad9163`, full ancestry, GPL-3.0 preserved) and merges only after owner acceptance.

Import procedure and safety gate: [MIGRATION-PLAN.md](../docs/migration/MIGRATION-PLAN.md). Import provenance: [WDTT-SOURCE-LEDGER.md](../docs/migration/WDTT-SOURCE-LEDGER.md).

The module must keep upstream GPL-3.0 notices; the original KeenWhite CLI is MIT. Keep distinct Go modules and process boundaries until license/security/build review is complete. Future OpenFlux, CSQTT and MAX integrations require separate design and license decisions. Do not introduce auto-start or an open SOCKS listener.
