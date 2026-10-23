# Multi-Provider Design (WDTT + OpenFlux + будущие) — KeenWhite 0.2.2-dev

Архитектурные обязательства для нескольких провайдеров в одном KeenWhite
(задание №13 этап E). Реализация переключения transport'ов сейчас НЕ
выполняется — только контракт, проверки конфликтов и документация.

## 1. Инварианты изоляции

| Требование | Механизм | Статус |
|---|---|---|
| Разные независимые процессы | каждый адаптер — свой процесс через собственный Runner; никакого общего рантайма | реализовано структурно |
| Разные loopback SOCKS-порты | `CheckProviderSet` (BIND_CONFLICT) + `catalog.ValidateBind` + порт-проба в `fullAdmission` каждого адаптера | тесты C03/C18 |
| Отдельные конфигурации и секреты | манифест+профиль per-provider; секреты только в файлах, никогда в argv/env/логах | реализовано |
| Независимый Stop/Status/Health | вызовы адресуются конкретному экземпляру адаптера; чужой PID не может быть завершён (generations+identity) | R05/R06 |
| Отдельные TCP/UDP evidence | per-provider evidence в манифесте; независимые оси (C12) | реализовано |
| Нет единого глобального hard-stop | лимит принадлежит провайдеру (WDTT: ровно 1800 с, `checkWDTTPolicy`); у OpenFlux будет своя лабораторная политика | закреплено тестами |
| Невозможность остановить чужой процесс | verifyOwnership + LabGrant; нет API «stop by PID» | R06 |
| Будущее переключение провайдера | выбор через конфигурацию владельца; НЕ автоматическое | явно отложено |

## 2. Конфликты между провайдерами (негативные проверки)

- `contract.CheckProviderSet` — дубликаты ID и общие бинды по набору (C02/C03).
- Каталог — статический реестр: bind-конфликты и дубликаты там же (C02/C03 каталог-слой).
- Порт-проба каждого адаптера ловит занятость в момент Prepare/Start (C13-стиль; A2.5).
- Resource admission — per-provider floor (`min_mem_available_mb`); суммарный
  бюджет устройства — вопрос будущего owner-GO (не выдумывается сейчас).

## 3. OpenFlux как второй провайдер (readiness, issue #5)

Черновик будущего манифеста (по исследованию KAS PR #199 / R7, источник
v0.13.0 `4f9c786`, локально воспроизведённая сборка arm64 28 311 712 B):

```json
{
  "provider": "openflux",
  "artifact": {"path": "…/openflux-linux-arm64", "sha256": "<pin 4f9c786 build>", "arch": "arm64"},
  "profile":  {"path": "…/openflux.conf", "required_files": ["encryption.key"]},
  "socks":    {"bind": "127.0.0.1:1083", "tcp": "BUILD_CONFIRMED", "udp": "SOURCE_CONFIRMED"},
  "resources":{"min_mem_available_mb": null, "kas_combined_min_ram_mb": null,
               "admission_source": "pending-RSS-measurement"},
  "lifecycle":{"hard_stop_seconds": 1800, "stop_grace_seconds": 10},
  "evidence": {"evidence_url": "https://github.com/saymer-alt/keenetic-auto-setup/pull/199"}
}
```

Отличия от WDTT: нет 1800-секундного upstream-лимита (1800 здесь — лабораторная
политика KeenWhite, не свойство бинарника); evidence TCP=BUILD_CONFIRMED
(локальная сборка VERIFIED), UDP=SOURCE_CONFIRMED; профиль direct+mailru+cupsonline аккаунтов не требует; GPL-3.0 → только отдельный процесс.

Риски (из R7): локальный DNS-резолв на клиенте (tunnel.go:382-395); UDP у
L4-exit идёт мимо upstream-proxy; RSS UNKNOWN — измерить до admission.

## 4. Не делается в этой версии

Автопереключение провайдеров, общий watchdog, агрегированный health,
acceptance-store (GAP из CONTRACT-COMPATIBILITY.md) — отдельные задачи.
