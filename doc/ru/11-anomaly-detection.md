# Глава 11: Обнаружение аномалий

## Обзор

Движок обнаружения аномалий melisai (`internal/model/anomaly.go`) применяет **37 пороговых правил** на основе рекомендаций Brendan Gregg и производственных best practices.

Правила с пометкой `(rate)` используют двухточечный сэмплинг — детектируют проблемы, происходящие *прямо сейчас*.

## 37 правил

### CPU (5 правил)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 1 | cpu_utilization | > 80% | > 95% |
| 2 | cpu_iowait | > 10% | > 30% |
| 3 | load_average | > 2x CPUs | > 4x CPUs |
| 4 | runqlat_p99 | > 10мс | > 50мс |
| 5 | cpu_psi_pressure | > 5% | > 25% |

### Память (8 правил)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 6 | memory_utilization | > 85% | > 95% |
| 7 | swap_usage | > 10% | > 50% |
| 8 | memory_psi_pressure | > 5% | > 25% |
| 9 | cache_miss_ratio | > 5% | > 15% |
| 10 | direct_reclaim_rate | > 10/с | > 1000/с |
| 11 | compaction_stall_rate | > 1/с | > 100/с |
| 12 | thp_split_rate | > 1/с | > 100/с |
| 13 | numa_miss_ratio | > 5% | > 20% |

### Диск (5 правил)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 14 | disk_utilization | > 70% | > 90% |
| 15 | disk_avg_latency | > 5мс | > 50мс |
| 16 | biolatency_p99_ssd | > 5мс | > 25мс |
| 17 | biolatency_p99_hdd | > 50мс | > 200мс |
| 18 | io_psi_pressure | > 10% | > 50% |

### Сеть (15 правил)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 19 | tcp_retransmits | > 10/с | > 50/с |
| 20 | tcp_timewait | > 5000 | > 20000 |
| 21 | network_errors_per_sec | > 10/с | > 100/с |
| 22 | conntrack_usage_pct | > 70% | > 90% |
| 23 | softnet_dropped | > 1/с | > 100/с |
| 24 | listen_overflows | > 1/с | > 100/с |
| 25 | nic_rx_discards | > 100 | > 10000 |
| 26 | tcp_close_wait | > 1 | > 100 |
| 27 | softnet_time_squeeze | > 1/с | > 100/с |
| 28 | tcp_abort_on_memory | > 0.1/с | > 1/с |
| 29 | irq_imbalance | > 5x | > 20x |
| 30 | udp_rcvbuf_errors | > 1/с | > 100/с |
| 31 | tcp_rcvq_drop | > 1/с | > 100/с |
| 32 | tcp_zero_window_drop | > 1/с | > 50/с |
| 33 | listen_queue_saturation | > 70% | > 90% |

### Контейнер (2 правила)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 34 | cpu_throttling | > 100 | > 1000 периодов |
| 35 | container_memory_usage | > 80% | > 95% |

### Система (1 правило)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 36 | gpu_nic_cross_numa | > 1 пара | > 1 пара |

### Другие (1 правило)

| # | Метрика | Warning | Critical |
|---|---------|---------|----------|
| 37 | dns_latency_p99 | > 50мс | > 200мс |

## Шкала здоровья

Оценка здоровья (0-100) складывается из штрафов USE-метрик и аномалий:

**USE-штрафы** (с весами): CPU 1.5x, Память 1.5x, Диск 1.0x, Сеть 1.0x, Контейнер 1.2x

**Штрафы за аномалии** (фиксированные): Critical = **-10 баллов**, Warning = **-5 баллов**

---

*Далее: [Глава 12 — Движок рекомендаций](12-recommendations.md)*
