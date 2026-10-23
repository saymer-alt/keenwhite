# Acceptance Store v0 (`internal/acceptance`) — 2026-10-23

Offline evidence store for KeenWhite verification runs (task 14 stage C).
It records what was tested, where, on which exact artifact, when, and with
which verdict — so that future emulator/physical runs have a native,
queryable place to land. Strictly offline: no network, no processes, no
router access, no secrets.

Связанные документы: `CONTRACT-COMPATIBILITY.md` (§ acceptance-v0 gap),
`EMULATOR-HANDOFF.md` (как записи будут появляться при интеграции).

## Схема записи — `keenwhite.acceptance/v1`

| Поле | Тип | Обязательное | Правило |
|---|---|---|---|
| `record_version` | int | да | константа `1`; неизвестные версии отвергаются (`ErrUnknownRecordVersion`) |
| `provider_id` | string | да | `^[a-z][a-z0-9-]{1,31}$` |
| `artifact_sha256` | string | да | 64 lowercase hex; доказательство действительно только для этого бинарника |
| `arch` | string | да | `arm64` \| `mipsle` (словарь KeenWhite) |
| `abi` | string | нет | verbatim; совместимость ABI не заявляется |
| `result_source` | string | да | `LOCAL_FAKE` \| `QEMU_USER` \| `KEENETIC_EMULATOR` \| `PHYSICAL` |
| `environment_id` | string | да | синтетический идентификатор стенда/гостя (без серийников и приватных адресов) |
| `environment_note` | string | нет | ≤256, напр. версия/хэш гостевого образа (синтетика) |
| `os_version` | string | нет | короткая метка («5.1.8», «host-linux») |
| `profile_label` | string | да | синтетическая метка профиля нагрузки |
| `config_ref` | string | нет | идентификатор конфигурации без секретов |
| `contract_version` | string | да | версия протестированного контракта (напр. `keenwhite.provider/v1`) |
| `test_type` | string | да | `TCP_E2E`, `UDP_E2E`, `RAM_BASELINE`, `RAM_SOAK`, `FAIL_CLOSED`, `REBOOT`, `LATENCY`, `THROUGHPUT`, `OTHER` |
| `measured_at` | RFC3339 | да | время измерения; будущее = повреждённая запись |
| `duration_sec` | int | нет | 0..2592000 |
| `result` | string | да | `PASS` \| `FAIL` \| `NOT_TESTED` \| `UNSUPPORTED` \| `UNKNOWN` — вердикт ЗАПУСКА, не уровень evidence провайдера |
| `cleanup_ok` | bool\|null | нет | зафиксирована ли проверка очистки ресурсов |
| `metrics` | map[string]float64 | нет | ≤32, только числовые (redacted), ключи `^[a-z][a-z0-9_]{1,31}$` |
| `artifacts` | []string | нет | ≤8 ссылок на воспроизводимые артефакты теста (без `..`) |
| `notes` | string | нет | ≤512 |

Отличия от исследовательской `acceptance-v0` (KAS): `device_id` заменён на
пару `result_source`+`environment_id` (KeenWhite различает стенды, а не
только физические устройства), `started_at`→`measured_at` (обязательное
время измерения для общего правила свежести), `result` расширен до пяти
вердиктов из C2, добавлены `arch`/`abi`, `config_ref`, `contract_version`,
`cleanup_ok`, `artifacts`. KAS-специфичные сущности не копировались.

## Главное правило (C3): синтетика никогда не становится физическим E2E

`EvidenceLevelOf(source)` — потолок того, что запись В ПРИНЦИПЕ может
подкрепить; `EffectiveEvidence(record, now, ttl)` применяет к потолку
вердикт запуска и свежесть:

| result_source | Потолок | Обоснование |
|---|---|---|
| `LOCAL_FAKE` | `BUILD_CONFIRMED` | проверяется поведение кода, не транспорт |
| `QEMU_USER` | `BUILD_CONFIRMED` | бинарник прошёл проверку под эмуляцией CPU |
| `KEENETIC_EMULATOR` | `BUILD_CONFIRMED` | результат уровня гостя; реального носителя нет |
| `PHYSICAL` | `E2E_VERIFIED` | только запуск на реальном устройстве; и только для `TCP_E2E`/`UDP_E2E` |

Дополнительные ограничения `EffectiveEvidence`: не-`PASS` вердикт →
`UNKNOWN`; просроченная/будущая дата → `UNKNOWN` (+ находка
`MEASURED_STALE`/`MEASURED_FUTURE`); физический `PASS` на не-E2E типе теста
→ `BUILD_CONFIRMED`.

Потолок — это **не присвоение**: пакет ничего не пишет в каталог/манифесты
и не выдаёт `E2E_VERIFIED` автоматически. Финальное подтверждение физического
E2E — процесс evidence с участием владельца.

## Хранилище

Раскладка: `<root>/<provider_id>/<artifact_sha256>/<record_id>.json`,
`record_id` детерминирован (`test_type-время UTC до секунды-профиль`).

Гарантии:
- **Атомарная запись**: temp-файл в том же каталоге + `rename`; сбой не оставляет половинной записи.
- **Append-only**: перезапись существующего `record_id` отказывается — переписывание истории свидетельств это порча, а не бухгалтерия.
- **Защита путей**: каждый элемент пути проверяется паттерном; traversal (`..`) невозможен по построению.
- **Лимиты**: запись ≤64 KiB; ≤32 метрик; ≤8 артефактов; `notes` ≤512.
- **Права**: файлы `0600`, каталоги `0700` (POSIX; на Windows не проверяется).
- **Без секретов**: свободные текстовые поля (`environment_note`, `config_ref`, `notes`) проверяются deny-листом (`password`, `token`, `PRIVATE KEY`, `AKIA…`, `ghp_`, …) — совпадение = отказ записи.
- **Версионирование**: неизвестный `record_version` — отказ; строгий decode (`DisallowUnknownFields`, запрет trailing JSON).
- **Битые записи сообщаются**: `ListRecords` возвращает их как `Problem`, никогда не молча пропускает.

## Честные ограничения

- Записи **не подписаны**: хранилище структурно защищено (декодирование,
  валидация, append-only, права), но обнаружить подмену содержимого
  валидной записи оно не может. Криптографическая целостность — отдельная
  задача, если потребуется.
- `ValidateRecord` проверяет синтаксис и политику, но не то, что тест
  «действительно прошёл как описано» — это процесс evidence-ревью, как и в
  исследовательской схеме.
- Совместимость точная (`CheckCompatibility`): свидетельство не наследуется
  между бинарниками (SHA), архитектурами, ABI, профилями и источниками.

## Использование

```go
store, err := acceptance.OpenStore("evidence/acceptance")
r := acceptance.Record{ /* … */ }
if f := acceptance.ValidateRecord(r, time.Now()); len(f) == 0 {
    path, err := store.WriteRecord(r, time.Now())
}
lvl, findings := acceptance.EffectiveEvidence(rec, time.Now(), 0)
```
