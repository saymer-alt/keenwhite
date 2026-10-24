# AGENTS.md — owner-wide engineering addendum

## Simplicity-first (owner decision, 2026-10-24)

Apply the [common saymer-alt engineering contract](https://github.com/saymer-alt/saymer-alt/pull/7) before designing new subsystems, UI, background managers, installers, removers or expensive CI. Always compare **existing/manual path**, **smallest safe change** (default), and **extended architecture**. Explicitly state what user problem it solves, untouched data/foreign resources, proven security invariants, new runtime dependencies, and time/test/maintenance cost. If a helper grows into an independent framework, or repeated heavy tests produce no new evidence, pause scope expansion and request an owner decision rather than adding yet another layer.

Maintain existing safety, legal provenance/upstream rules, and current working behavior. This instruction is documentation-only and does not permit rewriting existing code, changing production systems or releasing a version.

[KAS uninstall retrospective #225](https://github.com/saymer-alt/keenetic-auto-setup/issues/225) is a **negative example of overengineering**, not a best-practice architecture for reuse.
