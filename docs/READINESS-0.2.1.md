# KeenWhite 0.2.1-dev — матрица готовности (2026-10-23)

Статусная матрица v0.2.1-dev. Ничего из перечисленного не является
физической приёмкой; реальный Keenetic — `NOT_TESTED`.

| Компонент | Что проверить | Статус | Основание |
|---|---|---|---|
| KeenWhite Core | Контракты, конфигурации, Doctor | **WORKING (offline)** | catalog/provider tests; `validate/providers/doctor` — офлайн, `e2e = NOT_TESTED` |
| WDTT Adapter | Validate, Prepare, Start/Stop через mock | **WORKING (mock/lab)** | 18 harness-сценариев + R01–R12 + A2.1–A2.8; реальный запуск запрещён (`ErrForbiddenByPolicy`/`ErrGrantDenied`) |
| Security | PID ownership, hard-stop, LabGrant, ресурсы | **WORKING (offline)** | generations+identity (R05/R06), ровно 1800 с (R02), bounded TERM→KILL (R07), grant-модель (R04); grant выдаётся владельцем вне кода |
| Emulator Harness | 18 сценариев + регрессии | **WORKING (local-fake)** | отчёт v1 в CI-артефактах; `LOCAL_FAKE` only |
| ARM64 | Сборка и QEMU user-mode | **BUILD+QEMU OK** | кросс-сборка + офлайн smoke; не эмулятор Keenetic, не приёмка |
| MIPSel | Сборка и QEMU user-mode | **BUILD+QEMU OK** | аналогично; GOOS-сборка ≠ подтверждение железа |
| Реальный Keenetic | Всё вышеперечисленное на устройстве | **NOT_TESTED** | требует отдельного owner GO (issue #3) |

## Что явно НЕ заявлено

- Никакого E2E-трафика: все `E2E_VERIFIED` требуют свежего измерения с URL
  и проваливаются до `UNKNOWN` без него.
- WDTT 30-минутный (1800 с) lab hard-stop сохранён и закреплён кодом
  (`checkWDTTPolicy`) — не расширяется, не отключается, exit 75 остаётся в
  бинарнике.
- MIPSel-сборка не доказывает работоспособность на MIPSel-железе.
- Продуктовый релиз, теги beta/stable и пользовательские пакеты не
  выпускались.
