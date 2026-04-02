# Глава 19: Page Reclaim, Compaction и Transparent Huge Pages

## Проблема Page Reclaim

Linux не держит память без дела. Каждая свободная страница становится файловым кэшем, slab-кэшем или анонимной памятью. Неиспользуемая RAM -- это потраченная впустую RAM. Но когда приложение запрашивает память и свободных страниц нет, ядро должно *освободить* существующие страницы, прежде чем аллокация сможет завершиться.

Современные NVMe-накопители усугубляют ситуацию. Один NVMe-диск выдаёт 7 ГБ/с и миллионы IOPS. Приложения аллоцируют память с такой скоростью, что фоновый reclaim не справляется. Аллоцирующий поток блокируется, пока ядро синхронно освобождает страницы. Это **direct reclaim** -- главный источник необъяснимых всплесков задержки на системах с давлением по памяти.

Добавьте Transparent Huge Pages, и всё становится хуже. THP требуют 2 МБ *непрерывной* физической памяти. Когда память фрагментирована, ядро выполняет компакцию страниц для создания непрерывных блоков. Компакция дорогая, недетерминированная и происходит в пути аллокации. Аллокация 4 КБ за 100 нс может превратиться в аллокацию THP 2 МБ за 10 мс -- увеличение задержки в 100 000 раз.

melisai измеряет все три подсистемы -- reclaim, компакцию, THP -- используя двухточечный сэмплинг счётчиков `/proc/vmstat`.

## Водяные знаки памяти

Ядро использует три уровня водяных знаков (watermark) на каждую зону для принятия решения о reclaim:

```
   Общая память зоны
   +-------------------------------------------------+
   |            Используемая память (anon + cache)     |
   +--------------------------------------------------+ <- high watermark
   |        Свободные страницы -- kswapd останавливается здесь |
   +--------------------------------------------------+ <- low watermark
   |        Свободные страницы -- kswapd запускается здесь     |
   +--------------------------------------------------+ <- min watermark
   |     Зарезервировано -- территория direct reclaim  |
   |     (аллокации БЛОКИРУЮТСЯ здесь)                 |
   +--------------------------------------------------+
```

1. Свободных страниц выше `high` -- аллокации выполняются мгновенно.
2. Свободных страниц ниже `low` -- **kswapd** просыпается, выполняет reclaim в фоне.
3. Свободных страниц ниже `min` -- **direct reclaim**. Аллоцирующий поток сам сканирует и освобождает страницы.

Два sysctl управляют зазорами:

| Sysctl | По умолчанию | Эффект |
|--------|-------------|--------|
| `vm.min_free_kbytes` | ~67 МБ на 64 ГБ | Устанавливает watermark `min` для всех зон |
| `vm.watermark_scale_factor` | 10 (0.1%) | Зазор между min/low/high в % от размера зоны |

На сервере с 256 ГБ при `watermark_scale_factor=10` зазор между `low` и `min` составляет всего ~256 МБ. Всплеск аллокаций пробивает его за миллисекунды, вызывая direct reclaim до того, как kswapd успеет отреагировать.

## Direct Reclaim vs kswapd

**kswapd (фоновый)** -- поток ядра, по одному на NUMA-ноду. Просыпается при watermark `low`, сканирует LRU-списки, освобождает страницы. Без задержки для приложений.
Счётчики: `pgscan_kswapd`, `pgsteal_kswapd`.

**Direct reclaim (синхронный)** -- выполняется в контексте аллоцирующего потока.
Срабатывает при watermark `min`. Приложение блокируется до освобождения страниц.
Задержка: от 100 мкс до 100+ мс.
Счётчики: `pgscan_direct`, `pgsteal_direct`, `allocstall_*`.

Ключевые соотношения:

```
reclaim_efficiency = pgsteal / pgscan    (чем выше тем лучше, 1.0 = идеально)
direct_ratio      = pgscan_direct / (pgscan_direct + pgscan_kswapd)
```

- `direct_ratio = 0` -- весь reclaim фоновый. Здоровое состояние.
- `direct_ratio < 0.1` -- иногда direct reclaim. Допустимо.
- `direct_ratio > 0.3` -- kswapd не справляется. Влияние на задержку.
- `direct_ratio > 0.7` -- серьёзно. Приложения блокируются.

`allocstall_normal` -- самый прямой индикатор: каждое увеличение означает, что один поток вошёл в медленный путь.

## Как melisai измеряет это

melisai читает `/proc/vmstat` дважды -- в начале и конце сбора -- и вычисляет скорости (rate) на основе дельты:

```go
// internal/collector/memory.go
func (c *MemoryCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    vmstat1 := c.parseVmstatRaw()          // Первый сэмпл
    time.Sleep(cfg.Duration)                // По умолчанию: 10с
    vmstat2 := c.parseVmstatFull(data)      // Второй сэмпл + заполнение ReclaimStats
    c.computeReclaimRates(data, vmstat1, vmstat2, interval.Seconds())
}

func (c *MemoryCollector) computeReclaimRates(data *model.MemoryData,
    v1, v2 map[string]int64, secs float64) {
    if d := v2["pgscan_direct"] - v1["pgscan_direct"]; d > 0 {
        data.Reclaim.DirectReclaimRate = float64(d) / secs
    }
    if d := v2["compact_stall"] - v1["compact_stall"]; d > 0 {
        data.Reclaim.CompactStallRate = float64(d) / secs
    }
    if d := v2["thp_split_page"] - v1["thp_split_page"]; d > 0 {
        data.Reclaim.THPSplitRate = float64(d) / secs
    }
}
```

Двухточечный сэмплинг фиксирует *что произошло за окно сбора*, а не средние за всё время работы. 10-секундное окно ловит всплески, которые кумулятивные счётчики размывают.

### Пороги аномалий

| Метрика | Warning | Critical | Значение |
|---------|---------|----------|----------|
| `direct_reclaim_rate` | 10 страниц/с | 1000 страниц/с | Приложения блокируются при page reclaim |
| `compaction_stall_rate` | 1/с | 100/с | Аллокации блокируются на дефрагментации |
| `thp_split_rate` | 1/с | 100/с | Огромные страницы разбиваются, TLB thrashing |

## Компакция

Компакция памяти -- это дефрагментация ядра. Она перемещает страницы для создания непрерывных свободных блоков -- это нужно для огромных страниц, аллокаций ядра высокого порядка и CMA-регионов.

```
Фрагментировано: [used][free][used][free][used][free][used][free]
                  Сканер миграции ->                <- Сканер свободных
Компактировано:  [used][used][used][used][free][free][free][free]
```

Три счётчика:

| Счётчик | Значение |
|---------|----------|
| `compact_stall` | Аллокации, ожидавшие компакцию |
| `compact_success` | Запуски компакции, создавшие запрошенный порядок |
| `compact_fail` | Запуски компакции, которые не удались -- слишком фрагментировано |

Доля успехов = `compact_success / (compact_success + compact_fail)`. Ниже 0.5 означает, что компакция тратит CPU, но терпит неудачу. Ядро откатывается к меньшим аллокациям или запускает direct reclaim -- создавая многомиллисекундные задержки.

## THP: друг или враг?

Transparent Huge Pages отображают 2 МБ виртуальной памяти на одну физическую страницу 2 МБ вместо 512 x 4 КБ страниц. Преимущество: в 512 раз меньше записей TLB, выигрыш 5-15% производительности для нагрузок с большим объёмом памяти.

Цена заключается в трёх операциях:

**1. Аллокация при page fault (`thp_fault_alloc`)** -- При page fault ядро пытается аллоцировать 2 МБ непрерывной памяти. Если память фрагментирована, это запускает компакцию в пути обработки fault, в потоке приложения.

**2. Collapse (`thp_collapse_alloc`)** -- `khugepaged` сканирует существующие страницы 4 КБ и объединяет 512 непрерывных в огромную страницу. Фоновый процесс, но потребляет CPU.

**3. Разбиение (`thp_split_page`)** -- Когда ядру нужно освободить часть огромной страницы, оно разбивает её обратно на 512 маленьких страниц. Это требует TLB shootdown IPI к каждому CPU, на котором эта страница отображена. На 128-ядерной машине одно разбиение = 127 IPI. При 100 разбиениях/с это 12 700 IPI/с чистых накладных расходов.

## Режимы дефрагментации THP

| Режим | Поведение | Влияние на задержку |
|-------|----------|---------------------|
| `always` | Синхронная компакция при каждом THP fault | **Худший** -- неограниченные задержки |
| `defer` | Попытка, постановка компакции в очередь при неудаче, откат на 4 КБ | Низкое |
| `defer+madvise` | `defer` для большинства, синхронная для регионов `MADV_HUGEPAGE` | Низкое для большинства |
| `madvise` | THP только для регионов `MADV_HUGEPAGE` | **Нулевое** для не подписавшихся |
| `never` | Без дефрагментации THP | Нулевое |

**Рекомендация для production**: `defer+madvise`. Приложения, которым нужны огромные страницы (PostgreSQL, JVM, Redis), подписываются через `madvise(MADV_HUGEPAGE)`; всё остальное защищено от задержек компакции.

melisai читает из:
```
/sys/kernel/mm/transparent_hugepage/enabled   -> always/madvise/never
/sys/kernel/mm/transparent_hugepage/defrag    -> always/defer/defer+madvise/madvise/never
```

## JSON-вывод

```json
{
  "memory": {
    "thp_enabled": "always",
    "thp_defrag": "always",
    "min_free_kbytes": 67584,
    "watermark_scale_factor": 10,
    "dirty_expire_centisecs": 3000,
    "dirty_writeback_centisecs": 500,
    "reclaim": {
      "pgscan_direct": 48210,
      "pgscan_kswapd": 3841920,
      "pgsteal_direct": 41002,
      "pgsteal_kswapd": 3740100,
      "allocstall_normal": 312,
      "allocstall_movable": 18,
      "compact_stall": 89,
      "compact_success": 62,
      "compact_fail": 27,
      "thp_fault_alloc": 14320,
      "thp_collapse_alloc": 8410,
      "thp_split_page": 1205,
      "direct_reclaim_rate": 482.1,
      "compact_stall_rate": 8.9,
      "thp_split_rate": 12.5
    }
  }
}
```

Как читать: `direct_reclaim_rate=482.1` выше warning (10), ниже critical (1000). `compact_stall_rate=8.9` означает фрагментацию. `thp_enabled=always` с `thp_defrag=always` -- худшая комбинация для нагрузок, чувствительных к задержке. Доля успехов компакции `62/(62+27) = 70%` -- 30% потраченных впустую усилий.

## Диагностические примеры

### Здоровое состояние: нет давления reclaim

```json
{
  "reclaim": {
    "pgscan_direct": 0, "pgscan_kswapd": 120400,
    "pgsteal_kswapd": 118200, "allocstall_normal": 0,
    "compact_stall": 0, "thp_split_page": 3,
    "direct_reclaim_rate": 0, "compact_stall_rate": 0, "thp_split_rate": 0.3
  },
  "thp_enabled": "madvise", "thp_defrag": "defer+madvise",
  "watermark_scale_factor": 150
}
```

Весь reclaim через kswapd. Ноль direct reclaim, ноль задержек компакции. Эффективность kswapd: `118200/120400 = 98.2%`. THP в режиме madvise -- только подписавшиеся приложения получают огромные страницы.

### Критическое состояние: шторм THP под давлением памяти

```json
{
  "reclaim": {
    "pgscan_direct": 2841000, "pgscan_kswapd": 1420000,
    "pgsteal_direct": 890000, "pgsteal_kswapd": 1210000,
    "allocstall_normal": 14200,
    "compact_stall": 4200, "compact_success": 800, "compact_fail": 3400,
    "thp_fault_alloc": 420, "thp_split_page": 8900,
    "direct_reclaim_rate": 28410, "compact_stall_rate": 420, "thp_split_rate": 890
  },
  "thp_enabled": "always", "thp_defrag": "always",
  "watermark_scale_factor": 10
}
```

Всё плохо: `direct_reclaim_rate=28410` (critical), direct reclaim превышает kswapd, эффективность reclaim `890K/2841K = 31%`, доля неудач компакции 81%, и `thp_fault_alloc=420` против `thp_split_page=8900` означает, что THP работает в минус. melisai генерирует три рекомендации:

```
1. Direct reclaim активен -- увеличить резервы watermark
     sysctl -w vm.watermark_scale_factor=200
     sysctl -w vm.min_free_kbytes=131072

2. Обнаружены разбиения THP при THP=always -- переключить на madvise
     echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
     echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag

3. Обнаружены задержки компакции -- память фрагментирована
     echo 1 > /proc/sys/vm/compact_memory
     sysctl -w vm.extfrag_threshold=500
```

### Предупреждение: слишком медленный dirty writeback

```json
{
  "reclaim": {
    "pgscan_direct": 8400, "pgscan_kswapd": 620000,
    "direct_reclaim_rate": 84, "compact_stall_rate": 0, "thp_split_rate": 0
  },
  "dirty_expire_centisecs": 3000, "dirty_writeback_centisecs": 500
}
```

Умеренный direct reclaim (84/с), без проблем с компакцией или THP. Проблема: грязные страницы находятся в памяти 30 секунд (`dirty_expire_centisecs=3000`). Под давлением ядро должно синхронно записать их на диск перед освобождением.

## Руководство по настройке

### Шаг 1: Увеличить зазоры watermark

```bash
# По умолчанию: 10 (0.1%). Рекомендуется: 150-300 (1.5-3%)
sysctl -w vm.watermark_scale_factor=200
# Или установить напрямую (например, 256 МБ на сервере с 64 ГБ)
sysctl -w vm.min_free_kbytes=262144
```

Компромисс: более высокие watermark резервируют больше памяти. На 256 ГБ `watermark_scale_factor=200` резервирует ~5 ГБ. Это стоит того для устранения задержек.

### Шаг 2: Политика THP

```bash
echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag
```

### Шаг 3: Сброс грязных страниц

```bash
sysctl -w vm.dirty_expire_centisecs=1000     # 10с (по умолчанию: 30с)
sysctl -w vm.dirty_writeback_centisecs=100    # 1с  (по умолчанию: 5с)
sysctl -w vm.dirty_background_ratio=5         # начать сброс при 5%
sysctl -w vm.dirty_ratio=10                   # блокировать пишущих при 10%
```

### Шаг 4: Проактивная компакция (ядро 5.9+)

```bash
sysctl -w vm.compaction_proactiveness=20      # фоновая дефрагментация
echo 1 > /proc/sys/vm/compact_memory          # разовая ручная
```

### Шаг 5: Сделать настройки постоянными

```bash
cat >> /etc/sysctl.d/99-melisai-reclaim.conf << 'EOF'
vm.watermark_scale_factor=200
vm.min_free_kbytes=262144
vm.dirty_expire_centisecs=1000
vm.dirty_writeback_centisecs=100
EOF

# THP требует systemd-юнит:
cat > /etc/systemd/system/thp-madvise.service << 'EOF'
[Unit]
Description=Set THP to madvise
After=sysinit.target local-fs.target
[Service]
Type=oneshot
ExecStart=/bin/sh -c 'echo madvise > /sys/kernel/mm/transparent_hugepage/enabled'
ExecStart=/bin/sh -c 'echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag'
[Install]
WantedBy=basic.target
EOF
systemctl enable thp-madvise.service
```

## Когда использовать статические огромные страницы

THP удобны, но непредсказуемы. Для гарантированной аллокации огромных страниц без задержек компакции используйте статические (предварительно выделенные) огромные страницы:

- **Базы данных**: PostgreSQL `huge_pages=on`, Oracle SGA
- **DPDK**: требует предварительно выделенных огромных страниц
- **JVM**: `-XX:+UseLargePages` на критичных по задержке путях

```bash
# Зарезервировать 4096 огромных страниц (8 ГБ)
sysctl -w vm.nr_hugepages=4096
# Или при загрузке: hugepages=4096 в командной строке ядра
```

Статические огромные страницы резервируются при загрузке и не могут использоваться для других целей.
melisai отображает оба параметра:

```json
{ "huge_pages_total": 4096, "huge_pages_free": 1024, "thp_enabled": "madvise" }
```

Если `huge_pages_free == huge_pages_total`, у вас зарезервированы страницы, которые ничто не использует.

## Краткий справочник

| Симптом | Счётчик | Порог | Исправление |
|---------|---------|-------|-------------|
| Всплески задержки | `direct_reclaim_rate > 10` | W=10, C=1000 | Увеличить `watermark_scale_factor` |
| Задержки аллокации | `allocstall_normal > 0` | Любое увеличение | Увеличить `min_free_kbytes` |
| Разбиение THP | `thp_split_rate > 1` | W=1, C=100 | THP в режим `madvise` |
| Неудачи компакции | `compact_fail > compact_success` | Соотношение | `compact_memory`, `extfrag_threshold` |
| Давление грязных страниц | Высокий `pgscan_direct`, нет компакции | Контекст | Уменьшить `dirty_expire_centisecs` |

---

*Далее: [Глава 20 -- Оптимизация NUMA](20-numa-optimization.md)*
