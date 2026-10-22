# KeenWhite: transported reference contracts

**Authoritative development home: KeenWhite; KAS is only the optional installer.**

## MT-001 and MT-002 reference snapshot

The original, unchanged 32 files in [multitransport/](multitransport/) and [the Python validation script](../../tests/multitransport-contract.py) are an attributed snapshot from KAS Draft [PR #181](https://github.com/saymer-alt/keenetic-auto-setup/pull/181) at commit `3e37c61aac8502705cd4fb86e6eef74a33438967`, incorporating Draft [PR #175](https://github.com/saymer-alt/keenetic-auto-setup/pull/175) at commit `d87d80dfcc109cc978abeb335cb1e85490bfe2f4`.

Source repository `saymer-alt/keenetic-auto-setup` is MIT; names, KAS-specific prose, historic internal links, and the `$id` of the JSON schemas remain unchanged for transparent provenance. **Historical statements inside that snapshot are not a current KeenWhite release/status claim.** Specifically, the imported README's description of absent CI refers to its original design context; KeenWhite runs a separate, restricted workflow for the snapshot.

The reference contract is not automatically adopted by KeenWhite's native Go catalog. See [KW-C1 #4](https://github.com/saymer-alt/keenwhite/issues/4) to design explicit schema mapping, version negotiation and semantic tests.

Run isolated offline fixture checks (Python 3.12) with a separately installed test-only dependency:

```sh
python -m pip install 'jsonschema==4.26.0'
python tests/multitransport-contract.py --now 2026-10-21T12:00:00Z
```

These are the historical **33 synthetic checks**, not verification of real SHA files, PID ownership, TCP/UDP egress, DNS, IPv6, memory, or Keenetic compatibility. No transport or local listener is started.

## Owner-reviewed research dossiers

- [OpenFlux carrier independence and DNS policy](OPENFLUX-R7-DECISIONS.md) — [KW-O1 #5](https://github.com/saymer-alt/keenwhite/issues/5).
- [WDTT source import and acceptance gaps](../migration/WDTT-CI-ACCEPTANCE.md) — [KW-M1 #2](https://github.com/saymer-alt/keenwhite/issues/2) and [KW-M2 #3](https://github.com/saymer-alt/keenwhite/issues/3).

The original KAS [architecture Draft PR #171](https://github.com/saymer-alt/keenetic-auto-setup/pull/171) and [research Draft PR #199](https://github.com/saymer-alt/keenetic-auto-setup/pull/199) remain linked records. The former's old two-provider prerequisite for creating a separate repo was superseded by the owner's 2026-10-22 KeenWhite decision. Nothing here changes those source PRs.
