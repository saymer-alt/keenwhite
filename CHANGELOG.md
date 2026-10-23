# Changelog

Формат: реальные изменения репозитория; никаких обещаний будущего.
Версии `-dev` — desarrollo-срезы без релизов, тегов и пользовательских
бинарников (правило проекта).

## 0.2.2-dev — 2026-10-23 (задание №14; PR #33, #34, #35, #36, текущий)

### Исправлено
- **MT-001 совместимость (issue #4, повторно открыт аудитом)**: конвертер
  читал несуществующую форму (`architecture` строкой, оси `tcp`/`udp` с
  runtime-уровнями evidence) и отвергал оригинальные валидные фикстуры.
  Теперь — точное зеркало `provider-v0.schema.json`: массив архитектур
  `{arch,abi}` с документированным отображением (`aarch64→arm64`,
  `mipsel→mipsle`), статические `code`/`build` с монотонным инвариантом,
  полная сохранность metadata. Регрессия на оригинальных фикстурах +
  паритет с корпусом invalid-фикстур и 33 Python-проверками (PR #33).
- **Catalog WARNING/CLI (issue #32)**: `catalog.Validate` сворачивал все
  находки в строки, и CLI считал любой непустой список фейлом — легаси
  `E2E_VERIFIED` без даты был жёстким отказом. Теперь
  `catalog.ValidateDetailed`: ERROR → exit 1, WARNING → exit 0 с видимым
  предупреждением; эффективные уровни через общий `CheckEvidence` (TCP/UDP
  независимо), `doctor`/`providers` показывают эффективные уровни, JSON и
  текст семантически эквивалентны (PR #34).

### Добавлено
- **Offline Acceptance Evidence Store** `keenwhite.acceptance/v1`
  (`internal/acceptance`, PR #35): append-only, атомарная запись,
  паттерн-проверка путей, лимиты размеров, права 0600/0700, deny-лист
  секретов, строгий decode, отчёт о битых записях. Главное правило:
  **синтетический результат никогда не становится физическим E2E** —
  потолки по источнику (`LOCAL_FAKE`/`QEMU_USER`/`KEENETIC_EMULATOR` →
  `BUILD_CONFIRMED`; `PHYSICAL` + E2E-тип теста → `E2E_VERIFIED`), вердикт
  и свежесть применяются поверх. Ограничение: записи не подписаны.
- **Emulator handoff** (PR #36): `emulator.RunProof` — метка
  `KEENETIC_EMULATOR` только при полном доказательстве (guest id, версия и
  SHA256 образа, подтверждение исполнения в VM, exit code, guest PID,
  cleanup, проверенная на этом запуске изоляция); harness принимает только
  `local-fake` (прочие kind → провальный отчёт `UNVERIFIED`); честный
  `MockEnvironment`; переносимый `contracttest`-suite инвариантов для
  любой реализации `Environment`; [docs/EMULATOR-HANDOFF.md](docs/EMULATOR-HANDOFF.md).

### Качество
- **Целостность acceptance-хранилища (issue #38)**: атомарный append-only
  через `os.Link` (гонка Lstat→rename устранена; конкурентные писцы с одним
  record-id — ровно один победитель, `ErrRecordExists` остальным), привязка
  идентичности при чтении/листинге (`ErrIntegrity` для перемещённых/
  подменённых записей), symlink-политика (канонический корень, no-follow
  компоненты, `ErrUnsafePath`), fsync файла+каталога (каталог best-effort).
  17 новых integrity/стресс-тестов.
- Fuzz-таргеты `FuzzParseMTDocument` и `FuzzDecodeRecord` (инварианты
  трёхуровневой конверсии и строгого декодера; короткие сессии без
  находок). CI: `go vet`, `go test -race ./...`, harness-отчёт-артефакт,
  кросс-сборка arm64/mipsle; Python reference suite (33) — отдельный
  workflow по путям фикстур (в #33–#36 пути не менялись — прогон локальный).

### Осталось открытым
- #29 (реальное подключение эмулятора — ждёт проект эмулятора),
- #5 (OpenFlux adapter), #6 (KAS installer), #3 (физическая приёмка WDTT).
- Статус транспорта по-прежнему `NOT_TESTED`; релизов/тегов нет.

## 0.2.1-dev — 2026-10-23 (задания №10–№13)

- Provider Contract v1 + WDTT Adapter v0 (mock/lab only; admission на
  Prepare/Start, generations/ownership, bounded TERM→grace→KILL, LabGrant,
  hard-stop ровно 1800 с) — PR #14–#25.
- Security & CI hardening: required `offline-checks`, pinning actions по
  SHA, честные QEMU, честный Health — issues #18/#19/#20.
- `keenetic-emulator/v0` fail-closed заглушка; Emulator Harness S01–S18 +
  регрессии R01–R12.
- Единый `internal/contract` (CheckEvidence/CheckProviderSet), Compliance
  Suite C01–C18, multi-provider design без автопереключения — задания
  №13 (PR #30/#31).

## 0.1.0-dev / 0.2.0-dev — 2026-10 (задания до №10)

- Original offline CLI (`validate/providers/doctor/verify-wdtt`), строгий
  каталог `keenwhite.dev/v0alpha1`.
- WDTT PR #42 импорт с полной Git-историей GPL-3.0 (PR #1, 234/234 blobs
  identical), вложенный Go-модуль, QEMU user-mode проверки.
