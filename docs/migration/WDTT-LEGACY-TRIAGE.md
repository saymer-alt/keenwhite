# WDTT Legacy Triage — что в `transports/wdtt/` нужно KeenWhite (2026-10-23)

Выполнение оставшегося критерия Issue #2 (KW-M1): инвентаризация
импортированного WDTT-дерева. **Ничего не удалено и не переписано** — это
карта поддержки. Pin источника: `d39f9dad…` (PR #42 → KeenWhite PR #1,
234/234 blob-SHA). Лицензия всего дерева — GPL-3.0
(`transports/wdtt/LICENSE` сохранён дословно).

Классы: **ACTIVE** — нужен KeenWhite; **TEST-ONLY** — нужен только
испытаниям; **REFERENCE** — исторический источник/пример; **LEGACY** —
кандидат на архивирование (не удаляется); **UNKNOWN** — нужно исследование.

## Верхний уровень

| Путь | Класс | Обоснование / зависимости |
|---|---|---|
| `LICENSE` | ACTIVE | GPL-3.0 юридическая граница всего дерева; не трогать |
| `README.md` | REFERENCE | исходная документация upstream |
| `go.mod` / `go.sum` (корень `wg-turn-client`) | TEST-ONLY | корневой модуль-обёртка; целевой модуль — `go_client/` |
| `server.go` | LEGACY | одиночный серверный прототип эпохи до `hardened-relay/`; совместимые серверы строим отдельно на VPS |
| `app/` (87 файлов, gradle, Android UI) | LEGACY | Android-приложение; для headless Keenetic не нужно; зависит от Android SDK/gradle |
| `build.gradle.kts`, `settings.gradle.kts`, `gradle/`, `gradlew*`, `gradle.properties` | LEGACY | сборочная обвязка Android-приложения |
| `hardened-relay/` (8 файлов) | TEST-ONLY | усиленный серверный relay — нужен только для будущих VPS-испытаний; на роутере не живёт |
| `tools/` (5 файлов) | TEST-ONLY | вспомогательные инструменты разработки upstream |
| `scripts/` (3 файла) | TEST-ONLY | сборочные скрипты upstream (`build-go-lib.sh` и пр.); НЕ запускать без ревью |
| `provenance/` (31 файл) | REFERENCE | доказательства происхождения артефактов upstream; хранится для аудита цепочки |

## `go_client/` (69 файлов — целевой клиент,module `wg-turn-client`, Go 1.26)

| Группа | Представители | Класс | Обоснование |
|---|---|---|---|
| Ядро клиента | `main-компоненты`, `awgm_control.go`, `dispatcher.go`, `flags_compat.go` | ACTIVE | то, что собирает наш ARM64/MIPSel бинарник и чем управляет WDTT-адаптер (запуск, std-in/out протокол) |
| SOCKS/туннель | `group.go`, `group_raw.go`, socks/worker-компоненты | ACTIVE | локальный loopback SOCKS5 TCP/UDP — единственная поверхность, видимая Keenetic Proxy Client |
| TURN/сессии | `turn*.go`, `creds.go`, `creds_vkcalls.go` | ACTIVE (с оговоркой) | получение/обновление TURN-креденшелов; секреты не покидают процесс; для эмулятора — только синтетика |
| CAPTCHA | `captcha_v2.go`, `captcha_v2_slider.go` | ACTIVE (с оговоркой) | автоматическое решение slider-капчи; в эмуляторе отключается (нет реального VK) |
| Сеть/DoH | `doh.go`, `errhint.go` | ACTIVE | DNS-over-HTTPS внутри транспорта; конфликтует с «тестовый DNS» эмулятора — сверить при интеграции |
| Lab-режимы | `lab_security.go`, `lab_socks_mode_test.go`, `lab_getconf_status_test.go`, `lab_*_test.go` | TEST-ONLY | owner-lab ограничения (30-мин hard stop, TIME_LIMIT, exit 75) и их тесты — основа наших гарантий |
| Прочие тесты | `*_test.go` (существенная часть 69 файлов) | TEST-ONLY | гонки/юнит-тесты; выполняются WDTT import CI |
| Мусор/эксперименты | точечно | UNKNOWN | пофайловый проход не делался; перед любым удалением — отдельный аудит |

## Итог для Keenetic

Реально необходимо поддерживать: **`go_client/` (ядро+SOCKS+TURN+lab) +
`LICENSE`**; собранный бинарник — единственное, что попадает на устройство
через WDTT-адаптер (по LabGrant с pinned SHA256). Серверная сторона
(`hardened-relay/`, `server.go`) и Android (`app/`, gradle) в жизненный
цикл Keenetic не входят. Удалений нет; LEGACY-компоненты остаются в истории
до полевой приёмки (правило Issue #2).
