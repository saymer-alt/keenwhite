# Z AI — KW-BOOTSTRAP + KW-WDTT-MIGRATION (owner-authorized)

**Владелец 22.10.2026 прямо одобрил:** создать **самостоятельный** репозиторий `saymer-alt/keenwhite` и сделать его **единой основной площадкой для всей собственной white-list transport разработки Keenetic/Netcraze, начиная с WDTT**. Не откладывать создание проекта до двух принятых провайдеров: принято новое решение владельца, заменяющее прежний порог из KAS PR #171.

## Цель и правила

- Оригинальный GitHub repo `keenwhite` — **не форк** и не template WDTT/OpenFlux.
- Отдельный установщик KAS может ставить KeenWhite, но код KeenWhite не живёт в KAS.
- Код WDTT после source audit переходит в KeenWhite как **отдельный GPL-3.0 transport module**, с настоящим Git происхождением и сохранённой историей авторов (без squash). Исходный `wdtt-suite` хранится до завершения приёмки; его PR, tags, release URL остаются доступными.
- Новые адаптеры OpenFlux/CSQTT/Swinter/WhiteTransport пишутся/собираются здесь, при этом не копируются чужие лицензии или рабочие секреты.
- Никогда не отключать ограничения WDTT owner-lab, принудительно не включать транспорт, не менять router/VPS/KAS/Mihomo/routing/Proxy1/WAN/DNS/IPv6.

## Часть 1. Создать самостоятельный `keenwhite` СЕЙЧАС

1. Проверить через GitHub API, что `saymer-alt/keenwhite` ещё не существует. Скопировать из owner-provided KeenWhite ZIP исходники и файлы (без `.git` и локальных бинарников) в чистую директорию.
2. Убедиться в отсутствии `.env`, credentials, cookies, token, short-password transport URIs, внутренних IP, real config и raw logs.
3. Проверить `go test ./...`, `go test -race ./...`, `go vet ./...`, `make crossbuild`, `scripts/import-wdtt-history.sh --plan`. Программа пока только offline catalog/doctor — это честный текущий статус.
4. Создать **новый** публичный репозиторий `saymer-alt/keenwhite`, описание: `Independent white-list transport platform for Keenetic/Netcraze: first-party Go tooling, history-preserving WDTT module, pluggable carriers, safe diagnostics and optional KAS installation.` Topics: `keenetic`, `netcraze`, `entware`, `wdtt`, `socks5`, `multitransport`, `go`, `router`, `networking`.
5. Сделать новый root commit из локального исходного KeenWhite каркаса; push в `main`. Не использовать GitHub template от форков. Лицензия корня — MIT только для оригинального KeenWhite code, см. `docs/OWNERSHIP-AND-LICENSING.md`.
6. Включить Actions и защиту `main`: active ruleset with deletion+non_fast_forward+pull_request; approvals=0 при единственном сопровождающем, без bypass; запрещён прямой push. Никаких релизов, тегов или публикации бинарников.
7. Проверить реальный GitHub Actions run: vet, race tests, crossbuild ARM64/MIPSel (только компиляция).

**Если GitHub создание repo недоступно инструменту:** предоставить точный blocker; не заявлять, что repo создан; не заменять новым Issue вместо выполнения без попытки через доступные права.

## Часть 2. Перенести WDTT так, чтобы не потерять историю/лицензии

Исходник: https://github.com/saymer-alt/wdtt-suite, GPL-3.0, GitHub fork проекта hoaxisr/proxy-turn-vk-android.

**Опорная ревизия — комбинированный PR #42**, 
`d39f9dad91639a0f1ca321df72f3dc51ff0b0af0` в `release/wdtt-pr34-sec41-owner-20261022`.

1. Перед импортом получить `go_client`, `server.go`, корневой `go.mod`, Gradle/Android paths, лицензию, CI scripts и Git history ветки. Проверить, что выбранный commit действительно содержит PR #34 (18/1, совместимость) и PR #41 (редакция логирования), hard stop 30 минут и RAM admission. Не опираться только на описание PR — проверить code.
2. Провести secret scan **не только HEAD, но и всей импортируемой истории**; не печатать обнаруженные секреты в GitHub Issues/logs. При реальных credentials останавливать импорт и согласовывать безопасное сохранение истории/очистку, а не форсировать публикацию.
3. Создать отдельную ветку `migration/wdtt-pr42-d39f9dad9163` в KeenWhite и выполнить `scripts/import-wdtt-history.sh --plan`; после source audit `KEENWHITE_APPROVE_WDTT_IMPORT=<full SHA> KEENWHITE_HISTORY_AUDITED=yes scripts/import-wdtt-history.sh --apply`.
4. Это `git subtree add` **без `--squash`** в `transports/wdtt/`, с `transports/wdtt/LICENSE`. Отдельный nested Go-module допустим; не делать автоматический static-link GPL в MIT CLI.
5. Создать **Draft PR** в KeenWhite на импорт, provenance map + source SHA, проверки Git history, tests/build, license gate. Не merge, пока owner не подтвердит результаты.
6. **Все 27 исторических WDTT branches** записаны в `docs/migration/WDTT-SOURCE-LEDGER.md`. Они **не** становятся 27 активными PR в KeenWhite. Прочитать диффы полезных legacy-веток и оформить отдельные future tasks с source links; исходный WDTT repo пока не архивировать.
7. Старый экспериментальный WDTT prerelease и ссылки на него остаются доступными; никакой перепубликации под KeenWhite без физической приёмки.

## Часть 3. Перенести исследования и новые workstreams в KeenWhite

Распределение согласно `docs/migration/MIGRATION-PLAN.md`:

- WDTT: TCP/UDP correctness, 18/1 и 9/18, memory HWM and PSI, clean lifecycle, auth/logging redaction, BusyBox, source provenance, field egress checks.
- KeenWhite core: MT-001/MT-002 schemas + offline validator, status freshness and ownership, opt-in provider manager, reproducible build/test/release contracts.
- OpenFlux: pinned v0.13.0 source audit, carrier selection, VK corporate dependencies, local DNS caveat, baseline direct vs independent carrier tests, loopback SOCKS & no-leak acceptance.
- Secondary CSQTT, Swinter/MAX, WhiteTransport, whitelist-bypass: research/adapters/backlog only; do not imply wire interchangeability, no bulk imports.

В Issue #139 KAS сделать одно архивирующее решение «KeenWhite теперь основной репозиторий разработки, KAS — только installer» со ссылкой на новый repo. НЕ удалять исследовательские PR KAS #171/#175/#181/#199 и WDTT #34/#41/#42, до документированного cutover. В README всех затронутых исходных проектов при необходимости позже добавить ссылки на актуальный KeenWhite.

Создать начальную компактную структуру GitHub задач KeenWhite:

- `[KW-M1] WDTT GPL source history and exact PR42 provenance`;
- `[KW-M2] WDTT physical acceptance and security (18/1 + UDP + hard stop)`;
- `[KW-C1] MT-001 / MT-002 contract and semantic negative checks`;
- `[KW-O1] OpenFlux independent-carrier and DNS architecture`;
- `[KW-K1] KAS optional installer integration — defer runtime install`.

Максимум 5 хорошо связанных новых Issues; не создавать по Issue на каждую из 27 веток. Исходные исследования закрепить ссылками в одном индексном README.

## Acceptance и финальный ответ

Сообщить честно и раздельно:

1. URL созданного нового, не fork репозитория + root commit SHA + CI run + main protection.
2. WDTT source tree imported? Если нет — причина. Если да — Draft PR + commit, license/ancestry/security evidence. **Точный состав** (`go_client`, `server.go`, возможно Android legacy).
3. Количество мигрированных задач/решений, обратные ссылки в KAS/WDTT, нет ли потерь наработок.
4. Что реально проверено: offline Go tests, QEMU, physical hardware — не объединять эти категории.
5. Что не менялось: рабочие Keenetic, VPS, KAS installer, маршруты, существующий WDTT prerelease.
6. Следующие 3–5 задач по приоритетам.

Не запускать самостоятельные изменения рабочих устройств. Не делать merge экспериментов, теги/releases или перевод старого WDTT в архив без явного owner approval. После результата остановиться.
