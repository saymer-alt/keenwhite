# Emulator Handoff — подключение Keenetic Emulator к KeenWhite (2026-10-23)

Документ для агента, разрабатывающего эмулятор KeeneticOS 5.1.8: что должно
быть готово в эмуляторе, как KeenWhite его обнаружит и подключит, и какие
доказательства потребуются, чтобы результат теста получил метку
`KEENETIC_EMULATOR`. Контракт фиксирован: **`keenetic-emulator/v0`**
(`internal/emulator/contract.go`) — второй несовместимый API не создаётся.

Статус: заглушка `UnsupportedEnvironment` и честный `MockEnvironment`;
реальный `QemuEnvironment` — отдельная задача, когда эмулятор подтвердит
работоспособность гостевой среды (issue #29 остаётся открытым).

---

## 1. Минимальные требования к готовому эмулятору

Эмулятор считается готовым к подключению, когда он умеет всё из таблицы
ниже и его сетевая изоляция **проверена** (не «настроена»). До этого
момента KeenWhite не подключается: `Run` в harness отказывает любому
окружению кроме `local-fake` (см. §12).

| # | Требование | Операция контракта |
|---|---|---|
| 1 | Идентификация гостя: модель, версия KeeneticOS, архитектура, род эмулятора | `DetectEnvironment` |
| 2 | Ответ по всем 12 capabilities инвентаря, отсутствующее = `UNKNOWN` | `CheckCapabilities` |
| 3 | Живые admission-цифры: MemTotal/MemAvailable/DiskAvailable | `ReadResources` |
| 4 | Staging бинарника внутрь гостя с проверкой size+SHA256 | `StageArtifact` |
| 5 | Изолированный запуск staged-бинарника под жёстким таймаутом | `RunIsolatedTest` |
| 6 | Диагностика без секретов | `CollectDiagnostics` |
| 7 | Cleanup: terminate всех детей + удаление staged; идемпотентный | `Cleanup` |

## 2. Обнаружение версии KeeneticOS

`DetectEnvironment` возвращает `DeviceInfo.KeeneticOS` (например
`"5.1.8"`). Источник в гостевой системе: `/opt/ndm/ndm --version` или
файл версииfirmware, специфичный для порта — решает автор эмулятора;
контракт требует только стабильную строку версии. Ответ неизвестен →
пустая строка/`UNKNOWN`, не выдумка. Значение попадает в
acceptance-запись (`os_version`) и в отчёт.

## 3. Определение архитектуры

`DeviceInfo.CPUArch` — только `arm64` или `mipsle` (словарь KeenWhite;
соответствие MT `aarch64→arm64`, `mipsel→mipsle` — см.
`contract.MTArchToKeenWhite`). Прочие значения запрещены контрактом
v1-артефактов; эмулятор, портящийся под другую архитектуру, должен
сообщить `UNKNOWN`, а KeenWhite такой гость не примет.

## 4. Доступность `/opt`

Capability `OPT_PRESENT`. В KeeneticOS `/opt` — корень Entware; staging
бинарников идёт только внутрь `/opt` (или подкаталога), если хост-система
гостя этого не даёт — эмулятор сообщает `UNSUPPORTED`, и KeenWhite
помечает гостя непригодным для транспорта (fail-closed).

## 5. OPKG и Entware

Capability `OPKG_ENTWARE`. KeenWhite **не устанавливает пакеты** из
KeenWhite (автозапуск и watchdog запрещены политикой); эмулятор сам
обеспечивает готовую среду Entware. Ответ `SUPPORTED` означает «в госте
есть рабочие opkg+entware», а не «KeenWhite может что-то ставить».

## 6. Чтение RAM/MemAvailable

`ReadResources` → `Resources{MemTotalMB, MemAvailableMB,
DiskAvailableMB}`; источник в госте — `/proc/meminfo` (`MemAvailable`),
аналогично диску. Эти цифры — вход admission-проверки перед запуском;
ложные значения = фиктивный результат, что прямо запрещено.

## 7. PID и управление процессами

Capability `PID_CONTROL`. Требования контракта:
- `RunIsolatedTest` возвращает `TestResult` c exit code ВНУТРИ гостя;
- в `RunProof.GuestPID` записывается PID тестового процесса, наблюдаемый
  внутри гостя (аудит принадлежности запуска);
- `Cleanup` терминирует **все** дочерние процессы запуска (TERM→grace→
  KILL допускается на стороне эмулятора) и идемпотентен.

## 8. Проверка loopback

Capability `LOOPBACK`: `127.0.0.1` внутри гостя отвечает. SOCKS-бинд
транспортов остаётся строго `127.0.0.1` (правило v0/v1); эмулятор должен
давать петлевой интерфейс, но не «честный» LAN.

## 9. TCP/UDP в изолированной тестовой сети

Capabilities `ISOLATED_TCP`, `ISOLATED_UDP`, `TEST_DNS`. Ключевое
требование: сеть гостя — тестовая (isolated), без выхода в домашнюю/
производственную сеть; isolation подтверждается **проверкой**, а не
конфигурацией. `TestRequest.IsolatedNetwork=true` при неподтверждённой
изоляции → отказ (`ErrUnsupported`), никогда — выполнение. Реальный
WDTT/OpenFlux к интернету не подключается (запрет задания): серверная
сторона — только внутристендовые заглушки.

## 10. Механизм staging артефактов

`StageArtifact(ctx, localPath) → inDevicePath`:
1. чтение локального ELF (уже проверенного `artifact.VerifyWDTT` и
   `LabGrant`), копирование в гостя;
2. обязательная проверка копии: размер + SHA256; расхождение = ошибка
   staging, запуск не начинается;
3. путь внутри гостя возвращается вызывающему и передаётся в
   `TestRequest.StagedArtifact`.

Справочная реализация семантики — `MockEnvironment.StageArtifact`
(`internal/emulator/mock.go`): честный тестовый двойник, staging реальный,
исполнение всегда отказом.

## 11. Cleanup / rollback

`Cleanup(ctx)`:
- терминирует всех детей запуска (включая зависших гостевую сторону);
- удаляет все staged-артефакты;
- идемпотентен и безопасен после сбоя (двойной вызов — часть
  contract-тестов);
- результат фиксируется в `RunProof.CleanupOK`; без него метка
  `KEENETIC_EMULATOR` не присваивается.

## 12. Как KeenWhite запускает тесты

- Провайдер сценариев — `internal/harness` (18 сценариев S01–S18);
  сценарии не знают про эмулятор: им подставляется Environment.
- Сегодня `harness.Run` принимает только `Kind: "local-fake"` и ставит
  отчёту `result_source: LOCAL_FAKE`. Любой другой kind → отчёт FAIL
  (`UNVERIFIED`) — молчаливой переклейки меток нет (тест
  `TestUnverifiedEnvironmentKindFailsClosed`).
- Когда эмулятор готов: реализуется `QemuEnvironment` (отдельная задача),
  проходит `contracttest.RunSuite` + `RunStubSuite`-эквивалент, затем в
  harness добавляется ветка kind=`"keenetic-emulator"`, которая принимает
  окружение **только** с полным `emulator.RunProof` (§13).

## 13. JSON-отчёты, CI-артефакты и доказательство метки

Отчёт harness (`report_version 1`) пишется в `KW_HARNESS_REPORT`
(CI-артефакт), секретов не содержит по построению. Поле
`result_source` выводится из доказательства:

```
RunProof{
  GuestID, GuestImageVersion, GuestImageSHA256,   // что именно крутилось
  ExecutedInsideVM,                                // подтверждение супервизора гостя
  ExitCode, GuestPID, CleanupOK,                   // сам запуск и его уборка
  IsolationProven,                                 // изоляция проверена НА ЭТОМ запуске
}
emulator.ResultSourceFor(proof):  полный набор → KEENETIC_EMULATOR
                                  иначе        → LOCAL_FAKE
```

Каждый прогон эмулятора фиксируется записью в acceptance-хранилище
(`internal/acceptance`, `result_source=KEENETIC_EMULATOR`,
`environment_id`=`GuestID`, `environment_note`="image … sha256:…",
`cleanup_ok`). Потолок доказательства для эмулятора — `BUILD_CONFIRMED`
(результат уровня гостя; реального носителя нет) — `acceptance.EvidenceLevelOf`.

## Запреты (не ослабляются)

- Не выдумывать работающие endpoint'ы эмулятора; не запускать QEMU с
  неподтверждёнными настройками; не создавать фиктивные успешные
  результаты; не обходить проверки изоляции; не подключать реальный WDTT
  к интернету; не выдавать LOCAL_FAKE за KEENETIC_EMULATOR.

## Первые шаги, когда гость заработает

1. Реализовать `QemuEnvironment` поверх подтверждённой конфигурации;
2. Прогнать `contracttest.RunSuite` — все инварианты;
3. Добавить в `harness` ветку `keenetic-emulator` с обязательным
   `RunProof` и записью в acceptance-хранилище;
4. Прогнать S01–S18 внутри гостя; отчёт + записи — артефакты CI;
5. Только после этого — вопрос о физических испытаниях (отдельное
   разрешение владельца).
