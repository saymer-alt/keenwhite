# WDTT → KeenWhite: исходники, эксперименты, текущие контрольные точки

**Состояние: MIGRATION PLANNED, не выполнена.** Инвентаризация источников от 22 октября 2026 года. Исходник ещё остаётся в `saymer-alt/wdtt-suite` (GitHub fork от `hoaxisr/proxy-turn-vk-android`, GPL-3.0). Не трактовать имена веток как завершённые или проверенные возможности.

## Какие ревизии считать опорными

| Назначение | Ref | SHA / статус |
|---|---|---|
| Защищённая база исходного fork | `awg-client` | `096b3f4bf259` (22.10.2026) |
| Физический кандидат PR #34 (18/1, import/telemetry/password) | `integration/wdtt-owner-field-compat-20261021` | `b62bd1a7987713e8eb5ca468ef764d1c0ecac474`; Draft, не сливать молча |
| Логирование без утечки `TURN_CREDS` / `CAPTCHA_RESULT`, PR #41 | `feat/stdin-redact-logs` | `cb749bc79c989bb64cb5c561f1304c2fcc2c68f6`; Draft |
| **Составной кандидат для первого импорта**, PR #42 | `release/wdtt-pr34-sec41-owner-20261022` | **`d39f9dad91639a0f1ca321df72f3dc51ff0b0af0`**; Draft, CI/эмуляция есть, полного hardware acceptance нет |
| Тестовая версия из PR #23 (bounded pipe) | `feature/wdtt-bounded-pipe-owner-20261013` | `239df99dfb28`; исторический эксперимент, не обязательная часть текущего PR #42 |
| KAS экспериментальная интеграция | `saymer-alt/keenetic-auto-setup` PR #125 | `f76a34f3f9a2e6d52a383a6bf1e86ecf34874cdd`; KAS installer/supervisor — НЕ копировать без архитектурного ревью |

Опубликованный *экспериментальный*, не latest/pre-production WDTT PR34+PR41 релиз для воспроизводимости: 
`wdtt-owner-pr34-sec41-20261022-d39f9dad9163`. Он остаётся во внешнем WDTT-репозитории до явного перевыпуска и приёмки в KeenWhite. Прежние SHA256 артефактов можно использовать только при точном совпадении с теми же бинарниками; новая сборка требует новых SHA256.

## Все известные ветки исходного WDTT

`awg-client`, `awg-server`, `ci/wdtt-pr21-arm64-elf-20261013`, `feat/stdin-redact-logs`, `feature/qwdtt-anon18-singlehash-owner-20261021`, `feature/qwdtt-anonymous9-owner-20261012`, `feature/qwdtt-anonymous16-18-owner-20261012`, `feature/qwdtt-import-20261012`, `feature/wdtt-arm64-64slots-owner-20261012`, `feature/wdtt-bounded-pipe-owner-20261013`, `feature/wdtt-colon-private-import-owner-20261021`, `feature/wdtt-getconf-diagnostics-20261012`, `feature/wdtt-hardened-20261012`, `feature/wdtt-health-observe-20261013`, `feature/wdtt-keenetic-socks-20261012`, `feature/wdtt-legacy-short-pass-owner-20261021`, `feature/wdtt-lowmem-owner-20261013`, `feature/wdtt-socks-lifecycle-owner-20261012`, `feature/wdtt-tcp-idle-teardown-owner-20261012`, `feature/wdtt-telemetry-owner-20261012`, `feature/wdtt-udp-associate-20261012`, `feature/wdtt-worker-visibility-owner-20261021`, `integration/wdtt-combined-entware-accept-20261021`, `integration/wdtt-owner-field-compat-20261021`, `ops/wdtt-field-direct-download-20261021`, `release/wdtt-bounded-pipe-pr23-20261013`, `release/wdtt-pr34-sec41-owner-20261022`.

**Не создавать 27 продуктовых веток в KeenWhite автоматически.** История сохраняется двумя способами: (1) все ветки доступны в исходном публичном репозитории WDTT с immutable ссылками на SHA, (2) для нового поддерживаемого WDTT модуля переносится Git-история *выбранного кандидата* без squash. Полезные коммиты из других веток переносить только после diff/audit и отдельными PR. До полной проверки исходный репозиторий не архивировать/не удалять.

## Три результата, которые перенести в собственный проект

1. **Исходный транспортный клиент:** весь воспроизводимый код WDTT из кандидата PR #42 в отдельную директорию/модуль, с GPL и историей Git, без попытки переписать wire protocol ради брендинга.
2. **Собственный адаптер KeenWhite:** provider manifest, приватный профиль, binary/source SHA, `Start/Stop/Doctor` (позднее), PID/socket ownership, 30-min lab cutoff, проверки настоящего TCP и UDP, memory gates. Писать независимо от исходного WDTT; KAS PR #125 использовать как reference, не создавать второй watchdog.
3. **Доказательства и история исследований:** ссылки на CI, PR #14/#15/#21/#23/#24/#25/#34/#40/#41/#42, матрица `18/1` vs `18/2`, временные лимиты, негативные случаи, UDP ASSOCIATE, stale status, fail-closed, BusyBox и Entware. Личные конфиги и raw логи **не переносить**.

## Acceptance gates

- Первоначальный `git subtree` импорт в KeenWhite — **Draft PR**, не default branch.
- Оригинальный WDTT `LICENSE` и NOTICE остаются целы, история происхождения (`git log --follow`) проверяется.
- CI проверяет Go race/vet, отдельный Go-module/build-tools и pinned Entware SDK, QEMU; физические тесты отдельно.
- Никакого изменения действующих Proxy1/Policy2/Mihomo/DNS, домашнего NC-4210 или других роутеров.
- Никакого снятия owner-lab 30-минутного лимита; не считать ELF portable/build proof hardware acceptance.
- KAS не переносит компоненты KeenWhite автоматом; install/update/uninstall через отдельную интеграционную задачу.
