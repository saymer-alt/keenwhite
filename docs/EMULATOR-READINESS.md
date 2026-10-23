# Emulator Integration Readiness — KeenWhite 0.2.2-dev (2026-10-23, задание №16)

Read-only ревизия готовности инфраструктуры KeenWhite к подключению
будущего Keenetic Emulator (issue #29). Никакой QEMU не запускался,
транспорты не трогались; `QemuEnvironment` сознательно не реализуется до
подтверждения интерфейса настоящего эмулятора.

Связанные документы: [EMULATOR-HANDOFF.md](EMULATOR-HANDOFF.md) (требования
к эмулятору и RunProof), [ACCEPTANCE-STORE.md](ACCEPTANCE-STORE.md)
(целостность хранилища), issue [#29](https://github.com/saymer-alt/keenwhite/issues/29).

Масштаб статусов: **Инфраструктура** — есть ли в KeenWhite контракт/код/тесты
для данной операции; **Реальный прогон** — выполнялась ли операция против
настоящего гостя (сейчас — всегда нет).

| # | Операция | Инфраструктура | Реальный прогон | На чём основано |
|---|---|---|---|---|
| 1 | Обнаружение гостевой KeeneticOS | **READY** | NOT_TESTED | `Environment.DetectEnvironment` (контракт v0), инварианты contracttest, стаб возвращает честное `UNKNOWN` |
| 2 | Определение архитектуры | **READY** | NOT_TESTED | `DeviceInfo.CPUArch` (`arm64`/`mipsle`), таблица `MTArchToKeenWhite`, admission отказывает прочим |
| 3 | Определение доступной RAM | **READY** | NOT_TESTED | `Environment.ReadResources` → admission `min_mem_available_mb` (адаптер, тест C13) |
| 4 | Проверка `/opt` и Entware | **PARTIAL** | NOT_TESTED | capabilities `OPT_PRESENT`/`OPKG_ENTWARE` есть; установки пакетов из KeenWhite нет и не будет (политика) |
| 5 | Staging проверенного бинарника | **READY** | NOT_TESTED | `StageArtifact` с size+SHA256-верификацией; честный `MockEnvironment`; связка с `artifact.VerifyWDTT` и `LabGrant` задокументирована |
| 6 | Сбор PID и exit code | **PARTIAL** | NOT_TESTED | поля `TestResult.ExitCode` и `RunProof.GuestPID` есть; сборщик существует только со стороны будущего эмулятора |
| 7 | Доказательство сетевой изоляции | **BLOCKED** | NOT_TESTED | `RunProof.IsolationProven` должен поступить от **проверенного** исполнителя эмулятора; `RunIsolatedTest` обязан отказывать при неподтверждённой изоляции (тесты contracttest) |
| 8 | Контролируемое завершение процессов | **READY** | NOT_TESTED | `Cleanup` в контракте, идемпотентность в suite; bounded TERM→grace→KILL адаптера (R05/R06) |
| 9 | Сбор диагностического JSON | **READY** | NOT_TESTED | `CollectDiagnostics` — secret-free по построению (инвариант в contracttest) |
| 10 | Передача результатов в Acceptance Store | **READY** | NOT_TESTED | хранилище после #39/#41: атомарный append-only (включая межпроцессный тест), идентичность, no-symlink чтение; потолок для `KEENETIC_EMULATOR` — `BUILD_CONFIRMED` |

## Блокеры и их смысл

- **`QemuEnvironment` — BLOCKED** до подтверждения интерфейса настоящего
  эмулятора (issue #29). Никакой заглушки «по мотивам» не будет: новый
  исполнитель обязан пройти `contracttest.RunSuite` до включения в harness.
- **Isolation — BLOCKED**: изоляция сети доказывается проверкой на живом
  госте, а не конфигурацией. Пока — `RunProof.IsolationProven=false`,
  метка `KEENETIC_EMULATOR` недостижима, источник результатов —
  `LOCAL_FAKE` (см. ниже).
- Все операции с фактическим прогоном NOT_TESTED: реального гостя не было.

## RunProof: bool-флаги — не доказательства (задание №16)

`ExecutedInsideVM`, `IsolationProven`, `CleanupOK` не являются
доказательством только потому, что равны `true`. Метка
`KEENETIC_EMULATOR` присваивается **только** при полном наборе, привязанном
к конкретному запуску: GuestID, версия и SHA256 гостевого образа,
подтверждение исполнения в VM от супервизора гостя, exit code, GuestPID,
результат Cleanup, изоляция, проверенная **на этом запуске**
(`emulator.ResultSourceFor`). Негативные тесты:
`TestRunProof_BooleanFlagsAloneDoNotEarnLabel` (только булевы флаги /
частичные якоря → `LOCAL_FAKE`), `TestRunProof_LabelRequiresFullTuple`
(изъятие одного якоря из полного набора → `LOCAL_FAKE`),
`TestRunProofMissingPiecesStayLocalFake`.

**До появления проверенного исполнителя эмулятора источник любого
результата остаётся `LOCAL_FAKE`** — это обеспечено и gate'ом harness
(`Run` принимает только `local-fake`, прочие kind → провальный отчёт
`UNVERIFIED`), и таблицей потолков acceptance-хранилища.

## Готовый путь подключения (когда гость заработает)

1. Реализовать `QemuEnvironment` поверх подтверждённой конфигурации.
2. Прогнать `contracttest.RunSuite` — все инварианты.
3. Добавить в `internal/harness` ветку `keenetic-emulator` с обязательным
   полным `RunProof` и записью каждого прогона в acceptance-хранилище
   (`result_source=KEENETIC_EMULATOR`, `environment_id=GuestID`).
4. Прогнать S01–S18 внутри гостя; отчёт и записи — артефакты CI.
5. Физические испытания — только после отдельного разрешения владельца.

Версия остаётся `0.2.2-dev`; релизов и тегов нет. Issue #29 остаётся
открытым до реального подключения.
