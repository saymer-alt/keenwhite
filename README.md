# KeenWhite

**Independent white-list transport platform for Keenetic / Netcraze**

> Статус: **0.1.0-dev / экспериментальный проект**. Собственное Go-ядро выполняет offline-валидацию и read-only Doctor. **Исходники WDTT перенесены в `transports/wdtt/` и включены в `main` через [PR #1](https://github.com/saymer-alt/keenwhite/pull/1) с сохранением истории и GPL-3.0.** Изолированный CI клиентских/серверных Go-модулей и ARM64/MIPSel QEMU прошёл; **реального E2E на Keenetic и продуктового релиза KeenWhite пока НЕТ**.

KeenWhite — **наш единый поддерживаемый проект для сетевых транспортов в ограниченных сетях Keenetic/Netcraze**. Вместо множества самостоятельных экспериментальных форков здесь будут находиться собственное программное ядро, обслуживаемые транспортные модули (сначала WDTT), адаптеры, тесты, диагностика и общая дорожная карта. Различные сетевые протоколы не обязаны объединяться в один бинарник: самостоятельный репозиторий и единая система разработки совместимы с раздельными процессами и лицензиями.

Это независимый неофициальный проект. Он не связан с производителем роутеров.

## Граница проектов

| Проект | За что отвечает |
|---|---|
| **KeenWhite** | **Единый продукт и главный репозиторий**: оригинальный offline CLI, импортированный с историей WDTT GPL-3.0, будущие независимые адаптеры, контракты, CI и roadmap |
| **Keenetic Auto Setup (KAS)** | Установка и удаление KeenWhite как **необязательного** компонента, обновление, интеграция с существующим процессом supervisor |
| **WDTT Suite** | Исходный GPL-3.0 форк, лабораторные бинарники и прежние Issue/PR; остаётся архивом происхождения и обновлений. Не удалять до полевой приёмки |
| **OpenFlux** | Следующий транспорт; сначала собственный адаптер в KeenWhite, заимствование GPL-кода — только при отдельном решении с сохранением авторства и лицензии |
| **link-generators / Config Studio** | Создание и проверка пользовательских профилей, не runtime |
| **VPS-проекты** | Серверная сторона транспортов и инфраструктура выходных узлов |

**Правило:** весь наш НОВЫЙ код для white-list транспорта развивается здесь. Перенос WDTT делается не copy-paste, а поэтапным импортом Git-истории и независимым аудитом GPL-3.0. Остальные исходные форки служат источниками обновлений и решений, а не альтернативными площадками для новой разработки. Сторонний код не переписывает историю авторства.

## План концентрации всей разработки в KeenWhite

1. Опубликовать **новый** `saymer-alt/keenwhite`, без статуса fork.
2. **Выполнено:** WDTT PR #42 без squash перенесён в [`transports/wdtt/`](transports/wdtt/) через [KeenWhite PR #1](https://github.com/saymer-alt/keenwhite/pull/1), SHA источника `d39f9dad9163`; независимые вложенные Go/QEMU тесты — PASS. Поддержка на железе ещё не доказана.
3. Сохранить в KeenWhite реестр всех WDTT branches/issues/PR/release и экспериментов, но не объявлять 27 параллельных веток стабильными.
4. Сделать собственный адаптер и методы `status/doctor/start/stop` после ревью source/CPU/RAM/UDP и оборудования.
5. Переносить проверенные архитектурные решения из OpenFlux/CSQTT/Swinter/WhiteTransport в новые самостоятельные адаптеры; лицензии каждого компонента соблюдать.
6. KAS после проверки устанавливает **один самостоятельный продукт KeenWhite**, а не содержит его код.

**Материалы и подтверждения миграции:** [пошаговый план](docs/migration/MIGRATION-PLAN.md), [полный список WDTT-веток и точных исходных ревизий](docs/migration/WDTT-SOURCE-LEDGER.md), [лицензии и авторство](docs/OWNERSHIP-AND-LICENSING.md), [скрипт безопасного исторического импорта](scripts/import-wdtt-history.sh), [карта единого backlog](docs/migration/BACKLOG-MAP.md) → Issues [#2](https://github.com/saymer-alt/keenwhite/issues/2) [#3](https://github.com/saymer-alt/keenwhite/issues/3) [#4](https://github.com/saymer-alt/keenwhite/issues/4) [#5](https://github.com/saymer-alt/keenwhite/issues/5) [#6](https://github.com/saymer-alt/keenwhite/issues/6).

## Начало работы (только локальные проверки)

Для **KeenWhite CLI** нужен Go 1.22+; **WDTT-вложенные модули используют Go 1.25/1.26 и собираются отдельно**. На роутер KeenWhite пока не устанавливать.

```sh
go test ./...
go run ./cmd/keenwhite version
go run ./cmd/keenwhite providers -catalog examples/providers.json
go run ./cmd/keenwhite validate -catalog examples/providers.json
go run ./cmd/keenwhite doctor -catalog examples/providers.json -json
```

`validate` проверяет строгость JSON, ID, типы evidence, адреса **только 127.0.0.1**, порты, дубликаты, конфликт bind и policy-метаданные. `doctor` **не выполняет сетевых запросов, не открывает сокеты, не запускает transport, не читает приватные профили и не правит маршруты**. По этой причине поле TCP/UDP E2E всегда `NOT_TESTED`.

### Проверка WDTT-бинарника без запуска

Собственная команда KeenWhite `verify-wdtt` сверяет ELF-архитектуру и SHA256 с **независимо полученной** контрольной суммой, не запускает файл и не открывает соединения:

```sh
go run ./cmd/keenwhite verify-wdtt -file /secure/wt-client-linux-arm64 -sha256 YOUR_TRUSTED_64_HEX_SHA256 -arch arm64 -json
```

Проверка идентичности бинарника **не доказывает** работу транспорта, TCP/UDP E2E, отсутствие утечек или авторство источника хеша. [Ограничения и инструкция](docs/WDTT-ARTIFACT-PREFLIGHT.md).

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
- [Безопасность и ограничения](SECURITY.md)
- [Как добавлять код](CONTRIBUTING.md)
- [Переезд WDTT и объединение разработок](docs/migration/MIGRATION-PLAN.md)
- [Реестр экспериментальных веток WDTT](docs/migration/WDTT-SOURCE-LEDGER.md)
- [Границы лицензирования компонентов](docs/OWNERSHIP-AND-LICENSING.md)

Исходные KAS-исследования сохранены для прослеживаемости: [#139](https://github.com/saymer-alt/keenetic-auto-setup/issues/139), [PR #171](https://github.com/saymer-alt/keenetic-auto-setup/pull/171), [PR #175](https://github.com/saymer-alt/keenetic-auto-setup/pull/175), [PR #181](https://github.com/saymer-alt/keenetic-auto-setup/pull/181), [PR #199](https://github.com/saymer-alt/keenetic-auto-setup/pull/199). **Не переносить чужие открытые PR путём переписывания истории.**

## Лицензия

Оригинальный код KeenWhite — MIT ([LICENSE](LICENSE)). **Импортированный `transports/wdtt/` и производные изменения его исходников — GPL-3.0 с сохранёнными оригинальной лицензией и авторством.** Корневой MIT не перекрывает GPL; единый бинарник/линковка требуют дополнительной лицензионной проверки.

---

*EN: KeenWhite is an independent, non-fork toolkit for Keenetic/Netcraze. The original core is MIT; GPL-3.0 WDTT source was imported with preserved ancestry. Code build and QEMU tests passed, but real router/UDP acceptance and a stable product release are pending. KAS remains a separate optional installer.*
