# Глава 19: Page Reclaim, Compaction и THP

## Обзор

Когда Linux исчерпывает свободную память, ядро запускает **page reclaim** — освобождение страниц. Если kswapd (фоновый процесс) не успевает, начинается **direct reclaim** — приложение блокируется, ожидая освобождения памяти.

## Что собираем из /proc/vmstat

| Счётчик | Значение |
|---------|---------|
| `pgscan_direct` / `pgscan_kswapd` | Страницы, просканированные direct reclaim / kswapd |
| `pgsteal_direct` / `pgsteal_kswapd` | Страницы, освобождённые |
| `allocstall_normal` | Блокировки аллокаций |
| `compact_stall` / `compact_success` / `compact_fail` | Компакция памяти |
| `thp_fault_alloc` / `thp_collapse_alloc` | Аллокации THP |
| `thp_split_page` | Разбиения THP (вредно для TLB) |

Все счётчики используют **двухточечный сэмплинг** для вычисления rate (страниц/с).

## Правила аномалий

| Метрика | Warning | Critical | Значение |
|---------|---------|----------|---------|
| direct_reclaim_rate | > 10/s | > 1000/s | Приложения блокируются на reclaim |
| compaction_stall_rate | > 1/s | > 100/s | Фрагментация памяти |
| thp_split_rate | > 1/s | > 100/s | THP разбиваются — TLB thrashing |

## THP: друг или враг?

| Режим | Поведение | Когда использовать |
|-------|----------|-------------------|
| `always` | THP для всех | Только если нет fork-heavy нагрузок |
| `madvise` | THP только по запросу приложения | **Рекомендуется** для production |
| `never` | THP отключены | Для latency-critical систем |

## Рекомендации

1. **Direct reclaim** → `sysctl -w vm.watermark_scale_factor=200` + `vm.min_free_kbytes=131072`
2. **THP splits** → `echo madvise > /sys/kernel/mm/transparent_hugepage/enabled`
3. **Compaction** → `echo defer+madvise > /sys/kernel/mm/transparent_hugepage/defrag`

---

*Далее: [Глава 20 — Оптимизация NUMA](20-numa-optimization.md)*
