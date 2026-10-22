# Единый backlog KeenWhite после создания нового repo

Эта карта даёт новому репозиторию единую точку инженерных решений без удаления старой истории. Репозиторий `saymer-alt/keenwhite` создан 22.10.2026 (не fork); первичный backlog заведён как пять связанных Issues:

| Направление | KeenWhite Issue |
|---|---|
| WDTT GPL source history and exact PR42 provenance | [#2](https://github.com/saymer-alt/keenwhite/issues/2) — SOURCE IMPORTED через [PR #1](https://github.com/saymer-alt/keenwhite/pull/1), CI PASS; полевые тесты отдельно [#3](https://github.com/saymer-alt/keenwhite/issues/3) |
| WDTT physical acceptance and security (18/1 + UDP + hard stop) | [#3](https://github.com/saymer-alt/keenwhite/issues/3) |
| MT-001 / MT-002 contract and semantic negative checks | [#4](https://github.com/saymer-alt/keenwhite/issues/4) |
| OpenFlux independent-carrier and DNS architecture | [#5](https://github.com/saymer-alt/keenwhite/issues/5) |
| KAS optional installer integration — defer runtime install | [#6](https://github.com/saymer-alt/keenwhite/issues/6) |

Ссылки ниже — на исходные репозитории (источники решений), а не на дубликаты задач.

| Группа | Исходные ссылки | Новое направление KeenWhite |
|---|---|---|
| YPtun as WDTT library/RAW/worker/server source reference | [YPtun pinned v3.7.1 review](../research/YPTUN-WDTT-IMPLEMENTATION-20261022.md), [prior WDTT Suite #43](https://github.com/saymer-alt/wdtt-suite/issues/43) | KW-M2 [#3](https://github.com/saymer-alt/keenwhite/issues/3): source diff against gated WDTT PR #42, do not copy code or claim hardware acceptance |
| WDTT client behavior, privacy and physical tests | [WDTT roadmap #25](https://github.com/saymer-alt/wdtt-suite/issues/25), [P0 #14](https://github.com/saymer-alt/wdtt-suite/issues/14), [PR #34](https://github.com/saymer-alt/wdtt-suite/pull/34), [PR #41](https://github.com/saymer-alt/wdtt-suite/pull/41), [PR #42](https://github.com/saymer-alt/wdtt-suite/pull/42) | KW-M1 source migration and KW-M2 physical correctness/security |
| KAS optional WDTT installer/supervisor | [KAS PR #125](https://github.com/saymer-alt/keenetic-auto-setup/pull/125), [KAS #139](https://github.com/saymer-alt/keenetic-auto-setup/issues/139), [KAS #137](https://github.com/saymer-alt/keenetic-auto-setup/issues/137) | KW-K1 compatibility. KAS still installs other components |
| MultiTransport product boundaries | [KAS PR #171](https://github.com/saymer-alt/keenetic-auto-setup/pull/171), [KAS #164](https://github.com/saymer-alt/keenetic-auto-setup/issues/164) | KW-C1 new-core contract; owner decision supersedes separate repo criterion (no need for two working providers first) |
| Machine-readable contract | [KAS PR #175](https://github.com/saymer-alt/keenetic-auto-setup/pull/175) | KW-C1 migrate schemas with attribution/review |
| Semantic validator and negative tests | [KAS PR #181](https://github.com/saymer-alt/keenetic-auto-setup/pull/181) | KW-C1 migrate tests with traceable baseline, not blind cherry-pick |
| OpenFlux independent transport | [KAS #142](https://github.com/saymer-alt/keenetic-auto-setup/issues/142), [R7 PR #199](https://github.com/saymer-alt/keenetic-auto-setup/pull/199), [R7 review](https://github.com/saymer-alt/keenetic-auto-setup/pull/199#issuecomment-6343532855) | KW-O1 provider adapter, carrier source audit and no-leak plan |
| MAX / CSQTT | [KAS #162](https://github.com/saymer-alt/keenetic-auto-setup/issues/162), [KAS #141](https://github.com/saymer-alt/keenetic-auto-setup/issues/141) | Defer; separate licenses and wire protocols; use as references |
| Link generators / subscription/HWID | [link-generators](https://github.com/saymer-alt/link-generators) | Keep UI/config editing separate; optional import/export API later |

**Cutover definition**: Once KeenWhite has merged the approved core, a provenance-preserving WDTT module, and dedicated CI, NEW WHITELIST TRANSPORT R&D issues go into KeenWhite. KAS issues remain for installer/product boundary and any unresolved historical PR; old WDTT repo issues may remain open until status is reconciled, then close with links rather than deleting.

**Cutover update 2026-10-22:** historical WDTT source now lives in KeenWhite main, so *new* first-party white-list transport engineering belongs in KeenWhite Issues and PRs. Original WDTT Suite remains readable and retains PRs/releases/27 research branches; no changes to router installations. Remaining hardware/admission gates: #3 and #6.
