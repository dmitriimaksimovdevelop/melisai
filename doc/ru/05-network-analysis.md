# Глава 5: Анализ сети

## Обзор

`NetworkCollector` собирает данные из множества источников: счётчики интерфейсов, статистика TCP-протокола, состояния сокетов, таблица conntrack, softnet-статистика по CPU, распределение IRQ, аппаратные характеристики NIC и расширенная TCP-статистика.

## Функции

| Функция | Источник | Что собирает |
|---------|---------|-------------|
| `parseNetDev()` | `/proc/net/dev` | rx/tx bytes, packets, errors, drops по интерфейсам |
| `parseSNMP()` | `/proc/net/snmp` | CurrEstab, ActiveOpens, RetransSegs, InErrs |
| `parseSSConnections()` | `ss -s`, `ss -tn state close-wait` | TIME_WAIT и CLOSE_WAIT |
| `parseConntrack()` | `/proc/sys/net/netfilter/` | Использование conntrack-таблицы, дропы |
| `parseSoftnetStat()` | `/proc/net/softnet_stat` | Per-CPU: processed, dropped, time_squeeze |
| `computeIRQDistribution()` | `/proc/softirqs` | Дельта NET_RX прерываний по CPU |
| `parseNetstat()` | `/proc/net/netstat` | ListenOverflows, ListenDrops, PruneCalled, TCPAbortOnMemory |
| `enrichNICDetails()` | `/sys/class/net/`, `ethtool` | Драйвер, скорость, очереди, ring buffer, RPS, bond |

## Двухточечный сэмплинг

Коллектор делает два замера с интервалом (по умолчанию 1 сек) для вычисления скоростей:
- Первый замер: `/proc/net/dev`, `/proc/net/snmp`, `/proc/softirqs`
- Ожидание interval
- Второй замер + расчёт дельт (errors/sec, retransmits/sec, IRQ delta)

## Глубокая диагностика сети (Deep Network Diagnostics)

### Conntrack — таблица соединений

| Метрика | Warning | Critical | Значение |
|---------|---------|----------|---------|
| UsagePct > 70% | Да | > 90% | Таблица подходит к пределу — новые соединения будут отброшены |
| Drops > 0 | Да | Да | Соединения уже теряются |

**Исправление**: `sysctl -w net.netfilter.nf_conntrack_max=<текущий*2>`

### Softnet — обработка пакетов по CPU

Файл `/proc/net/softnet_stat` (hex-столбцы, по строке на CPU):

| Столбец | Имя | Значение |
|---------|-----|---------|
| 0 | processed | Обработано пакетов этим CPU |
| 1 | dropped | Пакеты отброшены (softirq не успевает) |
| 2 | time_squeeze | Бюджет softirq исчерпан |

**Любой ненулевой `dropped` = ядро теряет пакеты.** Причины:
- Один CPU обрабатывает все прерывания NIC (нет RPS/RSS)
- `net.core.netdev_budget` слишком мал (по умолчанию 300)

### Распределение IRQ

Дельта NET_RX по CPU за интервал. Если один CPU обрабатывает в 10 раз больше — это узкое место.

### TCP Extended — расширенные счётчики

| Счётчик | Значение | Действие |
|---------|---------|---------|
| `ListenOverflows` | Accept-очередь полна, SYN отброшен | Увеличить somaxconn, добавить SO_REUSEPORT |
| `ListenDrops` | То же + другие причины | Проверить скорость accept() приложения |
| `TCPAbortOnMemory` | Соединение прервано из-за нехватки памяти | Увеличить tcp_mem |
| `PruneCalled` | Ядро урезало TCP-буферы приёма | Увеличить tcp_mem |
| `TCPOFOQueue` | Пакеты не по порядку в очереди | Перепорядочивание или перегрузка сети |

### NIC Hardware — аппаратные характеристики

| Источник | Поле | Что показывает |
|----------|------|----------------|
| sysfs speed | Speed | Скорость линка (1000Mbps, 10000Mbps) |
| sysfs queues | RxQueues, TxQueues | Количество аппаратных очередей |
| sysfs rps_cpus | RPSEnabled | Распределяются ли пакеты по CPU |
| ethtool -i | Driver | Драйвер NIC (ixgbe, mlx5_core) |
| ethtool -g | RingRxCur/Max | Размер ring-буфера (текущий/максимальный) |
| ethtool -S | RxDiscards | Дропы на уровне NIC |

**Переполнение ring-буфера** (`RxDiscards > 0` при `RingRxCur < RingRxMax`):
```bash
ethtool -G eth0 rx 4096
```

## Глубокий анализ (Tier 2/3)

### tcpconnlat (Tier 2)
Измеряет время установления соединения (Handshake: SYN -> SYN/ACK -> ACK).

### tcpretrans (Tier 2/3)
Показывает каждую ретрансмиссию пакета в реальном времени.
- **Нативный eBPF (Tier 3)**: Работает без Python.

### gethostlatency (Tier 2)
Задержки DNS-запросов. Часто «тормоза сети» оказываются тормозами DNS.

## Проблемы TCP состояний

| Состояние | Число | Значение |
|-----------|-------|---------|
| TIME_WAIT < 1000 | Норма | Соединения остывают |
| TIME_WAIT > 50000 | Риск | Могут закончиться эфемерные порты |
| CLOSE_WAIT > 0 | Баг! | Приложение не закрывает сокет |
| CLOSE_WAIT > 100 | Критический баг | Утечка соединений |

## Sysctl параметры

| Параметр | Типичное | Назначение |
|----------|---------|-----------|
| `tcp_congestion_control` | `cubic` | Алгоритм перегрузки (bbr лучше для WAN) |
| `tcp_rmem` | `4096 131072 6291456` | Буфер приёма TCP (мин/дефолт/макс) |
| `tcp_wmem` | `4096 16384 4194304` | Буфер отправки TCP |
| `somaxconn` | 4096 | Макс. очередь listen |
| `tcp_mem` | `pages pages pages` | Глобальные лимиты TCP памяти |
| `tcp_max_tw_buckets` | 65536 | Макс. сокетов TIME_WAIT |
| `netdev_budget` | 300 | Макс. пакетов за цикл softirq |

## Правила обнаружения аномалий (сеть)

| Правило | Warning | Critical |
|---------|---------|----------|
| tcp_retransmits | 10/s | 50/s |
| tcp_timewait | 5000 | 20000 |
| network_errors_per_sec | 1/s | 100/s |
| conntrack_usage_pct | 70% | 90% |
| softnet_dropped | 1 | 10 |
| listen_overflows | 1 | 100 |
| nic_rx_discards | 100 | 10000 |

---

*Далее: [Глава 6 — Анализ процессов](06-process-analysis.md)*
