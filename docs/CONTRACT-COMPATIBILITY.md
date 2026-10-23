# Contract Compatibility — catalog / Provider v1 / MT-001-MT-002 (2026-10-23)

Единая карта соответствия трёх поколений контрактов KeenWhite (issue #4).
Общие проверки живут в `internal/contract` — едином источнике правил,
который используют каталог, Provider API и MT-совместимый слой.

| Документ | Версия | Где живёт |
|---|---|---|
| Catalog | `keenwhite.dev/v0alpha1` | `internal/catalog`, `examples/providers.json` |
| Provider Contract | `keenwhite.provider/v1`, `manifest_version: 1` | `internal/provider` (+ `internal/provider/wdtt`) |
| KAS MT-001/MT-002 | `schema_version: 1` (исследовательская линейка v0) | `docs/research/multitransport/` (бездельно импортировано, не меняется), `tests/multitransport-contract.py` (33 проверки) |
| Emulator | `keenetic-emulator/v0` | `internal/emulator` (заглушка) |

## 1. Матрица полей MT-001 provider manifest ↔ Provider v1

| MT-001 поле (schema_version=1) | Provider v1 поле | Семантика | Примечание |
|---|---|---|---|
| `schema_version` (const 1) | `manifest_version` (1) | эквивалент | неизвестные версии отказываются, не «тихо интерпретируются» |
| `provider_id` | `provider` | идентичны | один regex `^[a-z][a-z0-9-]{1,31}$` в обоих |
| `adapter_version` (semver) | — (нет поля) | WARNING при конвертации | адаптер в KW — код в `internal/provider/wdtt`; версия = контрактная `APIVersion()` |
| `server_wire` (explicit id) | — (в каталоге: `wire`) | WARNING | в v1-манифесте не носится; каталог остаётся реестром |
| `server_class` | — (в каталоге: `server_class`) | WARNING | аналогично |
| `architecture` — МАССИВ `{arch, abi}` (`aarch64\|armv7\|mips\|mipsel\|x86_64`, 1..8, unique) | `artifact.arch` (`arm64\|mipsle`) + `artifact.path` на одну архитектуру | **явное отображение имён** | только `aarch64→arm64`, `mipsel→mipsle` (`contract.MTArchToKeenWhite`); прочие варианты сохраняются в диагностике verbatim; при 2+ кандидатах выбор не делается (ERROR `MT_ARCH_AMBIGUOUS`); ABI переносится verbatim (`ArchABI`) и совместимости не заявляет |
| `capabilities{name:{code,build}}` — ДВА независимых статических атрибута (`PRESENT\|ABSENT\|UNKNOWN`, инвариант build=PRESENT ⇒ code=PRESENT) | `socks.tcp` / `socks.udp` (Evidence) | **таблица соответствия** (ниже, §1.1) | статика никогда не становится runtime evidence; `E2E_VERIFIED` из MT-001 недостижим по построению; отсутствие оси = `UNKNOWN` + WARNING (не PASS) |
| `inbound_bind` (ровно `127.0.0.1:port`, MT-порт 1..65535) | `socks.bind` (`127.0.0.1`, порт 1024..65535) | эквивалент по хосту, KW строже по портам | порт <1024 валиден для MT-001 → WARNING `MT_BIND_OUTSIDE_V1`; финальное решение — admission Provider v1 |
| `resource_requirements.admission_min_mb` (int\|null, 64..16384) | `resources.min_mem_available_mb` | эквивалент | `null` → 0 (без floor) |
| `resource_requirements.admission_source` | `resources.admission_source` | эквивалент | строка-происхождение политики |
| `resource_requirements.measured` (+`measurement_ref`) | — (нет поля) | UNKNOWN-finding | KAS-измерения не переносятся; KW меряет сама в Prepare/Start |
| `resource_requirements` KAS ≥512MB combined | `resources.kas_combined_min_ram_mb` | **только документация** | политика KAS #137; глобально на другие транспорты не переносится (правило #6 задания) |
| `artifact_provenance.sha256` | `artifact.sha256` | эквивалент | 64 hex в обоих |
| `artifact_provenance.source_commit` | — (нет поля) | WARNING | хранится в миграционном леджере |
| `artifact_provenance.artifact_path` | `artifact.path` | эквивалент | |
| `configuration_reference` | `profile.path` | эквивалент | секретная конфигурация вне манифеста — правило сохранено |
| — (отсутствует) | `profile.required_files`, `lifecycle.hard_stop_seconds/stop_grace_seconds`, `evidence.{url,measured_at,ttl}` | KW-дополнения | hard-stop обязателен (WDTT: ровно 1800); MT-специфичного hard-stop нет |
| MT `status-v0` (verdicts PASS/FAIL/UNKNOWN/NOT_TESTED/UNSUPPORTED/STALE) | `provider.Status`/`Health` + Evidence | отображение | PASS↔свежий `E2E_VERIFIED`; STALE→даунгрейд в `UNKNOWN`; NOT_TESTED↔`UNKNOWN`; listener/process/E2E — раздельные состояния |
| MT `acceptance-v0` (полевые записи по artifact+device+profile+duration) | — (нет хранилища) | **GAP → отдельная задача** | не выдумано; фикстуры остаются исследовательскими |

### 1.1. Таблица соответствия статических capabilities (задание 14, этап A)

MT-001 `capabilities` — статические атрибуты исходников (`code`) и собранного
бинарника (`build`). Правило соответствия — единственное и документированное
(`contract.mtCapabilityEvidence`); повышение и выдумывание уровней исключены:

| code | build | → Evidence Provider v1 | Обоснование |
|---|---|---|---|
| PRESENT | PRESENT | `BUILD_CONFIRMED` | в исходниках и собрано в артефакт — максимум статики |
| PRESENT | UNKNOWN | `SOURCE_CONFIRMED` | исходники подтверждают, состояние сборки неизвестно |
| PRESENT | ABSENT | `UNSUPPORTED` | в исходниках есть, в этом артефакте — нет |
| ABSENT | PRESENT | — (отказ) | нарушение монотонного инварианта схемы |
| ABSENT | ABSENT | `UNSUPPORTED` | артефакт статически лишён возможности |
| ABSENT | UNKNOWN | `UNKNOWN` | исходники отрицает, сборка не подтверждаема |
| UNKNOWN | PRESENT | — (отказ) | нарушение монотонного инварианта схемы |
| UNKNOWN | UNKNOWN | `UNKNOWN` | данных недостаточно |
| UNKNOWN | ABSENT | `UNSUPPORTED` | артефакт лишён возможности независимо от исходников |

Оси: `socks_tcp` → `socks.tcp`, `socks_udp` → `socks.udp`; остальные
возможности (`tun`, …) сохраняются verbatim в `Map.Capabilities`.
Ось, отображённая в `UNSUPPORTED`, делает манифест неисполняемым
(`Runnable=false`); `UNKNOWN` остаётся структурно исполнимым и отказывает
позже на runtime-admission (fail-closed).

### 1.2. Три уровня готовности конверсии (задание 14, этап A2)

`ParseMTDocument` возвращает `MTConversion` с тремя раздельными флагами:

| Уровень | Смысл |
|---|---|
| `Parsed` | документ соответствует provider-v0.schema.json (успешный разбор исходника) |
| `Partial` | часть полей не переносится 1:1 (есть WARNING/UNKNOWN/UNSUPPORTED-находки) |
| `Runnable` | безопасно строить исполняемый Provider v1 манифест: нет ERROR, ровно одна исполнимая архитектура, обе оси заявлены и не `UNSUPPORTED` |

`ParseMTProviderManifest` (совместимая обёртка) возвращает ошибку тогда и
только тогда, когда `Runnable=false`.

## 2. Единые правила (реализованы в `internal/contract`)

- `CheckEvidence(level, url, measured, ttl, now)` — одна и та же логика для
  каталога, манифеста провайдера и MT-конвертации: E2E требует URL + дату в
  прошлом внутри TTL; будущая дата = повреждённые данные; устаревание
  понижает E2E до `UNKNOWN`; `SOURCE_CONFIRMED` никогда не повышается.
- `CheckProviderSet(refs)` — дубликаты Provider ID и конфликты SOCKS-биндов
  по всему набору провайдеров.
- `catalog.ValidateDetailed(c, now)` (issue #32) — типизированные находки
  каталога: структурные нарушения = ERROR (exit 1), непроверяемые E2E-заявления
  = WARNING с даунгрейдом эффективного уровня в `UNKNOWN` (exit 0, видимое
  предупреждение; политика «accept, downgrade, warn»). TCP/UDP — независимо,
  через тот же `CheckEvidence`. `catalog.Validate` остаётся строковой
  проекцией для совместимости.
- `ParseMTDocument(doc, now)` — полный фиделити-разбор MT-001 (см. §1.2);
  `ParseMTProviderManifest(doc, now)` — совместимая обёртка: неизвестная
  `schema_version` = отказ; каждое непереносимое поле — WARNING (никакой
  тихой потери); доказательственная дисциплина — п.1.

## 3. Найденные расхождения и их статус

1. Каталог не проверял дату/TTL измерений (только URL) — **исправлено**:
   общий `CheckEvidence` подключён к Provider v1; каталогу добавлено
   опциональное `evidence_measured_at` (обратная совместимость: старые
   файлы валидны, E2E без даты → WARNING).
2. Provider v1 не проверял принадлежность манифеста адаптеру на уровне
   правил набора — identity уже закреплена в адаптере (#25), наборная
   проверка ID добавлена в `contract.CheckProviderSet`.
3. MT `capabilities` (code/build) vs KW Evidence (runtime) — семантический
   сдвиг задокументирован; соответствие зафиксировано явной таблицей (§1.1),
   `E2E_VERIFIED` из статики MT-001 недостижим по построению.
4. Дублирование правил (catalog vs provider E2E) устранено — единая точка.
5. `acceptance-v0` (полевые записи) в KW отсутствует — отдельная задача,
   не придумывается.
6. «Циклические ссылки/зависимости» — в текущей схеме таких конструкций
   нет (каталог и манифесты одноуровневые); проверка не выдумывается.
   Если появятся — спецификация + отдельная задача.
7. **Переоткрытие #4 (аудит задания 14) — исправлено**: конвертер читал
   MT-001 неверной формы (`architecture` строкой, оси `tcp`/`udp` с
   runtime-уровнями evidence) и отвергал оригинальные валидные фикстуры
   (`cannot unmarshal array into Go struct field mtDoc.architecture`).
   Реализовано точное зеркало provider-v0.schema.json: массив архитектур,
   статические code/build, монотонный инвариант, полная сохранность
   metadata (§1.1–§1.2). Регрессия: `internal/contract/mtfidelity_test.go`
   на оригинальных фикстурах + паритет с корпусом invalid-фикстур
   (Go-вердикты совпадают с `tests/multitransport-contract.py`).

## 4. Правила совместимости (этап C задания)

1. Версия исходной схемы фиксируется (`schema_version == 1`). 2. Неизвестная
версия = отказ. 3. Потеря полей — только с WARNING-находкой per field.
4. `UNKNOWN` никогда не становится PASS. 5. `SOURCE_CONFIRMED` никогда не
повышается. 6. KAS ≥512MB — только строка `admission_source`/документация.
7. Единый процесс для WDTT+OpenFlux не требуется (раздельные процессы —
архитектурное правило). 8. Существующие конфигурации KeenWhite валидны
(новое поле каталога опционально).
