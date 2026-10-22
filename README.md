# KeenWhite

**Independent white-list transport platform for Keenetic / Netcraze**

> Статус: **0.1.0-dev / экспериментальный проект с планом миграции WDTT**. Это новый самостоятельный репозиторий, не GitHub fork. Пока реализованы локальная проверка каталога транспортов и read-only Doctor. **Рабочий VPN-туннель и WDTT-код ещё НЕ включены в этот пакет (main); перенос исходников подготовлен в Draft [PR #1](https://github.com/saymer-alt/keenwhite/pull/1) с сохранением полной Git-истории и GPL-лицензии — слияние только после приёмки владельцем.**

KeenWhite — **наш единый поддерживаемый проект для сетевых транспортов в ограниченных сетях Keenetic/Netcraze**. Вместо множества самостоятельных экспериментальных форков здесь будут находиться собственное программное ядро, обслуживаемые транспортные модули (сначала WDTT), адаптеры, тесты, диагностика и общая дорожная карта. Различные сетевые протоколы не обязаны объединяться в один бинарник: самостоятельный репозиторий и единая система разработки совместимы с раздельными процессами и лицензиями.

Это независимый неофициальный проект. Он не связан с производителем роутеров.

## Граница проектов

| Проект | За что отвечает |
|---|---|
| **KeenWhite** | **Единый продукт и главный репозиторий**: собственный runtime/CLI, WDTT после поэтапного исторического импорта, другие поддерживаемые адаптеры, контракты, тесты, релизы и roadmap |
| **Keenetic Auto Setup (KAS)** | Установка и удаление KeenWhite как **необязательного** компонента, обновление, интеграция с существующим процессом supervisor |
| **WDTT Suite** | Исходный GPL-3.0 репозиторий и лабораторный релиз; **первый кандидат на исторический перенос в KeenWhite**, затем архив происхождения и предыдущих PR, без удаления до полной приёмки |
| **OpenFlux** | Следующий транспорт; сначала собственный адаптер в KeenWhite, заимствование GPL-кода — только при отдельном решении с сохранением авторства и лицензии |
| **link-generators / Config Studio** | Создание и проверка пользовательских профилей, не runtime |
| **VPS-проекты** | Серверная сторона транспортов и инфраструктура выходных узлов |

**Правило:** весь наш НОВЫЙ код для white-list транспорта развивается здесь. Перенос WDTT делается не copy-paste, а поэтапным импортом Git-истории и независимым аудитом GPL-3.0. Остальные исходные форки служат источниками обновлений и решений, а не альтернативными площадками для новой разработки. Сторонний код не переписывает историю авторства.

## План концентрации всей разработки в KeenWhite

1. Опубликовать **новый** `saymer-alt/keenwhite`, без статуса fork.
2. Перенести **WDTT Suite PR #42** с полной историей в `transports/wdtt/` на отдельной миграционной ветке; до ревью сам WDTT не меняется.
3. Сохранить в KeenWhite реестр всех WDTT branches/issues/PR/release и экспериментов, но не объявлять 27 параллельных веток стабильными.
4. Сделать собственный адаптер и методы `status/doctor/start/stop` после ревью source/CPU/RAM/UDP и оборудования.
5. Переносить проверенные архитектурные решения из OpenFlux/CSQTT/Swinter/WhiteTransport в новые самостоятельные адаптеры; лицензии каждого компонента соблюдать.
6. KAS после проверки устанавливает **один самостоятельный продукт KeenWhite**, а не содержит его код.

**Подготовленные материалы миграции:** [пошаговый план](docs/migration/MIGRATION-PLAN.md), [полный список WDTT-веток и точных исходных ревизий](docs/migration/WDTT-SOURCE-LEDGER.md), [лицензии и авторство](docs/OWNERSHIP-AND-LICENSING.md), [скрипт безопасного исторического импорта](scripts/import-wdtt-history.sh), [карта единого backlog](docs/migration/BACKLOG-MAP.md) → Issues [#2](https://github.com/saymer-alt/keenwhite/issues/2) [#3](https://github.com/saymer-alt/keenwhite/issues/3) [#4](https://github.com/saymer-alt/keenwhite/issues/4) [#5](https://github.com/saymer-alt/keenwhite/issues/5) [#6](https://github.com/saymer-alt/keenwhite/issues/6).

## Начало работы (только локальные проверки)

Нужен Go 1.22+ на рабочей машине. На роутер устанавливать пока не нужно.

```sh
go test ./...
go run ./cmd/keenwhite version
go run ./cmd/keenwhite providers -catalog examples/providers.json
go run ./cmd/keenwhite validate -catalog examples/providers.json
go run ./cmd/keenwhite doctor -catalog examples/providers.json -json
```

`validate` проверяет строгость JSON, ID, типы evidence, адреса **только 127.0.0.1**, порты, дубликаты, конфликт bind и policy-метаданные. `doctor` **не выполняет сетевых запросов, не открывает сокеты, не запускает transport, не читает приватные профили и не правит маршруты**. По этой причине поле TCP/UDP E2E всегда `NOT_TESTED`.

### Сборка под Keenetic (пока только проверка кросс-компиляции)

```sh
make crossbuild
```

Цели Go: `linux/arm64` и `linux/mipsle` (`GOMIPS=softfloat`). **Успешная компиляция не означает подтверждённую работу на аппаратном Keenetic**, пригодность транспортов под эту архитектуру или одобрение к установке. Первый выпуск не содержит установщика, автозапуска и продуктового релиза.

## Первые кандидаты

| Провайдер | Этап | Интерфейс (резерв, не listener) | Чего не хватает |
|---|---|---|---|
| WDTT Suite | `lab` | `127.0.0.1:1081` | стабильное физическое TCP/UDP E2E, ownership, интеграция |
| OpenFlux | `candidate` | `127.0.0.1:1083` | обновление v0.13.0, local DNS, egress, RAM, подтверждённый server wire |

Позже: CSQTT (только VK-резерв, не независимый оператор), MAX, другие carriers — по решениям владельца и результатам реальных испытаний. Названия в каталоге не подразумевают встроенных клиентов или готовых установщиков.

## Принципы

1. **Независимость KAS Core:** его обычная установка работает без KeenWhite.
2. **Fail-closed по проверяемому E2E:** открытый порт SOCKS5 не равен работающему выходу.
3. **Никаких скрытых маршрутов:** не менять Proxy1, WAN, DNS/IPv6, Mihomo или firewall без отдельного разрешения владельца.
4. **Проверяемые ограничения ресурсов:** min-RAM только при наличии явного источника; текущий порог KAS+WDTT 512 MB не распространяется автоматически на OpenFlux.
5. **Нулевые секреты в GitHub и логах:** токены, профили, cookies и HWID остаются вне репозитория.
6. **Подтверждение лицензий:** все новые адаптеры пишутся самостоятельно, копирование чужого кода — только после licence review.

## Документация

- [Архитектура и границы ответственности](docs/ARCHITECTURE.md)
- [Первый roadmap](docs/ROADMAP.md)
- [Реестр upstream-исследований и лицензий](docs/UPSTREAM-SOURCES.md)
- [Разбор YPtun v3.7.1: WDTT API, TURN, RAW SOCKS5, WDTT Plus](docs/research/YPTUN-WDTT-IMPLEMENTATION-20261022.md) — source-review, без переноса кода
- [Безопасность и ограничения](SECURITY.md)
- [Как добавлять код](CONTRIBUTING.md)
- [Переезд WDTT и объединение разработок](docs/migration/MIGRATION-PLAN.md)
- [Реестр экспериментальных веток WDTT](docs/migration/WDTT-SOURCE-LEDGER.md)
- [Границы лицензирования компонентов](docs/OWNERSHIP-AND-LICENSING.md)

Исходные KAS-исследования сохранены для прослеживаемости: [#139](https://github.com/saymer-alt/keenetic-auto-setup/issues/139), [PR #171](https://github.com/saymer-alt/keenetic-auto-setup/pull/171), [PR #175](https://github.com/saymer-alt/keenetic-auto-setup/pull/175), [PR #181](https://github.com/saymer-alt/keenetic-auto-setup/pull/181), [PR #199](https://github.com/saymer-alt/keenetic-auto-setup/pull/199). **Не переносить чужие открытые PR путём переписывания истории.**

## Лицензия

Оригинальный исходный код KeenWhite — MIT (см. [LICENSE](LICENSE)). **Когда WDTT будет перенесён в `transports/wdtt/`, его GPL-3.0 код сохранит GPL и notices в собственном подкаталоге.** Не считать, что корневой MIT автоматически лицензирует WDTT либо OpenFlux.

---

*EN: KeenWhite is an independent, non-fork, white-list transport platform for Keenetic/Netcraze. It will consolidate first-party code and proven WDTT development with preserved GPL provenance. Its current implementation is offline-only; neither live WDTT operation nor hardware acceptance is included. KAS remains a separate optional installer.*
