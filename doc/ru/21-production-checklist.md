# Глава 21: Чек-лист настройки для production

## Назначение

Это краткая справочная карточка по каждому sysctl и параметру ядра, которые melisai собирает, анализирует и рекомендует. Каждый пункт включает параметр, его значение по умолчанию в ядре, рекомендуемое значение для production, когда его менять и какая метрика melisai вызывает рекомендацию.

Все изменения `sysctl` **временные** по умолчанию. Для сохранения после перезагрузки запишите их в `/etc/sysctl.d/99-melisai.conf` и выполните `sysctl -p /etc/sysctl.d/99-melisai.conf`.

---

## Планировщик CPU

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `kernel.sched_latency_ns` | 24000000 (24 мс) | **6000000** (6 мс) | Интерактивные / чувствительные к задержке нагрузки; при высоком `runqlat_p99` | `runqlat_p99` |
| `kernel.sched_min_granularity_ns` | 3000000 (3 мс) | **750000** (0.75 мс) | То же; меньшая гранулярность = более справедливое планирование на загруженных CPU | `runqlat_p99` |
| `kernel.sched_numa_balancing` | 0 | **1** | Мульти-NUMA системы, где процессы мигрируют между нодами; снижает удалённый доступ к памяти | `cpu_utilization`, дисбаланс нагрузки между NUMA-нодами |

**Примечания:**
- `sched_latency_ns` управляет тем, как долго планировщик ждёт перед вытеснением задачи. Более низкие значения улучшают отзывчивость, но увеличивают накладные расходы на переключение контекста.
- `sched_min_granularity_ns` устанавливает минимальный квант времени. Слишком низкое значение для нагрузок, ориентированных на пропускную способность, тратит CPU на планирование.
- `sched_numa_balancing` включает автоматическую миграцию страниц на NUMA-ноду, где работает обращающийся поток. Оставьте 0 на одно-сокетных машинах.

---

## Управление памятью

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `vm.swappiness` | 60 | **10** (базы данных), **30** (общий production) | Когда swap используется и задержка важна; базы данных практически никогда не должны свопить | `swap_usage` |
| `vm.dirty_ratio` | 20 | **10** | Когда наблюдаются задержки при записи; высокий dirty ratio означает большие всплески writeback | `memory_utilization`, анализ грязных страниц |
| `vm.dirty_background_ratio` | 10 | **5** | Обеспечивает более ранний запуск фонового writeback, предотвращая накопление грязных страниц | `memory_utilization` |
| `vm.overcommit_memory` | 0 (эвристика) | **2** (строгий) | Production базы данных (PostgreSQL, Redis), где OOM kill недопустим | `memory_utilization` |
| `vm.min_free_kbytes` | варьируется (~67 МБ) | **131072** (128 МБ) | Системы с >16 ГБ RAM; предотвращает задержки direct reclaim при всплесках аллокаций | `memory_utilization` |
| `vm.watermark_scale_factor` | 10 | **200** | Когда обнаружены события direct reclaim; расширяет зазор между watermark свободных страниц | `memory_psi_pressure` |
| `vm.dirty_expire_centisecs` | 3000 (30 с) | **1500** (15 с) | Уменьшает время нахождения грязных страниц в памяти до пригодности к writeback | `memory_utilization` |
| `vm.dirty_writeback_centisecs` | 500 (5 с) | **300** (3 с) | Как часто просыпается flusher-поток; короче = более плавный I/O, немного больше накладных расходов | `memory_utilization` |
| `vm.zone_reclaim_mode` | 0 | **0** (по умолчанию) или **1** (NUMA-локальный) | Установите 1, только если ваша нагрузка строго требует NUMA-локальной аллокации и допускает задержки reclaim; 0 правильно для большинства нагрузок | `memory_psi_pressure` |

### Transparent Huge Pages (THP)

| Настройка | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|-----------|-------------|-------------|--------------|-----------------|
| `/sys/kernel/mm/transparent_hugepage/enabled` | `always` | **`madvise`** | Нагрузки, чувствительные к задержке (базы данных, JVM); `always` вызывает задержки компакции | `memory_utilization`, статус THP в отчёте |
| `/sys/kernel/mm/transparent_hugepage/defrag` | `always` | **`defer+madvise`** | То же; `always` запускает синхронную компакцию при page fault | `memory_psi_pressure` |

**Примечания:**
- `overcommit_memory=2` требует установки `overcommit_ratio` (обычно 80-95%). Лимит общего commit = swap + RAM * ratio / 100.
- Слишком высокий `min_free_kbytes` тратит память; слишком низкий вызывает задержки аллокаций. Масштабируйте пропорционально общему объёму RAM.
- THP в режиме `madvise` позволяет приложениям подписаться через `madvise(MADV_HUGEPAGE)`, избегая неожиданных всплесков задержки.

---

## Сеть -- стек TCP

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `net.ipv4.tcp_congestion_control` | cubic | **bbr** | Всегда на ядрах >= 4.9; BBR лучше справляется с bufferbloat и потерями в сети | `tcp_retransmits` |
| `net.core.default_qdisc` | pfifo_fast | **fq** | Требуется для BBR; Fair Queue обеспечивает поддержку pacing | `tcp_retransmits` |
| `net.core.somaxconn` | 4096 | **65535** | Высоконагруженные серверы с переполнением очереди listen | `listen_overflows` |
| `net.ipv4.tcp_max_syn_backlog` | 1024 | **8192** | Защита от SYN flood или высокая скорость установки соединений | `listen_overflows` |
| `net.ipv4.tcp_rmem` | 4096 131072 6291456 | **4096 131072 16777216** | Каналы с высокой пропускной способностью или высокой задержкой; максимальный буфер должен покрывать BDP | `tcp_retransmits` |
| `net.ipv4.tcp_wmem` | 4096 16384 4194304 | **4096 131072 16777216** | То же что tcp_rmem; буферы отправки должны соответствовать BDP | `tcp_retransmits` |
| `net.core.rmem_max` | 212992 | **16777216** (16 МБ) | Ограничивает максимальный буфер приёма; должен быть >= максимума tcp_rmem | `tcp_retransmits` |
| `net.core.wmem_max` | 212992 | **16777216** (16 МБ) | Ограничивает максимальный буфер отправки; должен быть >= максимума tcp_wmem | `tcp_retransmits` |
| `net.ipv4.ip_local_port_range` | 32768 60999 | **1024 65535** | Высокая скорость соединений вызывает исчерпание эфемерных портов | `tcp_timewait` |
| `net.ipv4.tcp_tw_reuse` | 2 | **1** | Разрешает переиспользование сокетов TIME_WAIT для новых исходящих соединений | `tcp_timewait` |
| `net.ipv4.tcp_fin_timeout` | 60 | **15** | Уменьшает время нахождения сокетов в FIN_WAIT_2; быстрее освобождает ресурсы | `tcp_timewait` |
| `net.ipv4.tcp_slow_start_after_idle` | 1 | **0** | Постоянные соединения (HTTP/2, gRPC) не должны сбрасывать cwnd после простоя | `tcp_retransmits` |
| `net.ipv4.tcp_fastopen` | 1 | **3** | Включает TFO для клиента (1) и сервера (2); экономит 1 RTT при переподключении | `tcp_retransmits` |
| `net.ipv4.tcp_syncookies` | 1 | **1** (оставить включённым) | Защита от SYN flood; всегда должен быть включён в production | `listen_overflows` |
| `net.ipv4.tcp_notsent_lowat` | -1 (без ограничений) | **131072** (128 КБ) | Снижает потребление памяти для приложений с множеством простаивающих соединений (HTTP/2, websocket) | `memory_utilization` |
| `net.ipv4.tcp_mtu_probing` | 0 | **1** | Включает обнаружение Path MTU; предотвращает проблемы с маршрутизаторами-чёрными дырами, дропающими ICMP | `tcp_retransmits` |

### TCP Keepalive

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `net.ipv4.tcp_keepalive_time` | 7200 (2 ч) | **300** (5 мин) | Быстрое обнаружение мёртвых соединений; важно за балансировщиками нагрузки | `tcp_close_wait` |
| `net.ipv4.tcp_keepalive_intvl` | 75 | **15** | Интервал между keepalive-пробами после начального таймаута | `tcp_close_wait` |
| `net.ipv4.tcp_keepalive_probes` | 9 | **5** | Количество неподтверждённых проб до разрыва соединения | `tcp_close_wait` |

### Память TCP

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `net.ipv4.tcp_mem` | автовычисление | **Увеличить в 2-4 раза** если PruneCalled > 0 | Когда ядро начинает обрезать очереди приёма TCP из-за давления по памяти | `tcp_abort_on_memory` |

**Примечания:**
- `tcp_mem` указывается в страницах (не в байтах). Три значения: low / pressure / max. Когда использование превышает "pressure", ядро начинает обрезку. Пример: `1048576 2097152 4194304` (4-8-16 ГБ).
- Формула настройки буферов: `требуемый_буфер = пропускная_способность_бит/с * RTT_секунды`. Канал 1 Gbps с RTT 100 мс требует буферы 12.5 МБ.
- `tcp_tw_reuse=1` безопасен для исходящих соединений. Никогда не используйте удалённый `tcp_tw_recycle`.
- `tcp_fastopen=3` требует поддержки в приложении (опция сокета `TCP_FASTOPEN`).

---

## Сеть -- обработка пакетов

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `net.core.netdev_budget` | 300 | **600** или выше | Когда `softnet_time_squeeze` > 0; бюджет опроса NAPI исчерпан | `softnet_time_squeeze` |
| `net.core.netdev_budget_usecs` | 2000 | **8000** | То же; даёт больше времени на цикл softirq для обработки пакетов | `softnet_time_squeeze` |
| `net.core.netdev_max_backlog` | 1000 | **10000** | Когда `softnet_dropped` > 0; переполнение очереди backlog на CPU | `softnet_dropped` |

### Conntrack

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `net.netfilter.nf_conntrack_max` | 65536 | **Удвоить текущее** при использовании > 70% | Таблица отслеживания соединений приближается к лимиту; вызывает отброс новых соединений | `conntrack_usage_pct` |

### Таблица соседей (ARP/NDP)

| Параметр | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|----------|-------------|-------------|--------------|-----------------|
| `net.ipv4.neigh.default.gc_thresh1` | 128 | **2048** | Среды Kubernetes / большие подсети с множеством подов; предотвращает "neighbour table overflow" | `network_errors_per_sec` |
| `net.ipv4.neigh.default.gc_thresh2` | 512 | **4096** | То же; мягкий лимит до срабатывания сборки мусора | `network_errors_per_sec` |
| `net.ipv4.neigh.default.gc_thresh3` | 1024 | **8192** | То же; жёсткий лимит -- записи сверх этого немедленно отклоняются | `network_errors_per_sec` |

### Аппаратная настройка NIC

| Настройка | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|-----------|-------------|-------------|--------------|-----------------|
| `ethtool -G <iface> rx <max>` | по умолчанию вендора | **Максимальный размер кольцевого буфера** | Когда обнаружены `rx_discards` или потери в кольцевом буфере | `nic_rx_discards` |
| `ethtool -K <iface> tso on gro on` | обычно включено | **Убедиться что включено** | TCP Segmentation Offload и Generic Receive Offload снижают нагрузку на CPU | `cpu_utilization`, `softnet_time_squeeze` |

**Примечания:**
- `netdev_budget` управляет тем, сколько пакетов CPU может обработать за один цикл опроса NAPI. Увеличение жертвует справедливостью задержки ради пропускной способности.
- Conntrack загружается автоматически при наличии правил NAT iptables/nftables. На нодах Kubernetes таблица conntrack может заполняться быстро. Мониторьте с помощью `conntrack -C`.
- Переполнения таблицы соседей вызывают сообщения `neighbour table overflow` в dmesg и приводят к перемежающимся сбоям сетевого соединения.
- Проверьте максимальный размер кольцевого буфера с помощью `ethtool -g <iface>`. Текущие значения отображаются в отчёте melisai в разделе `nic_details`.

---

## Дисковый I/O

| Настройка | По умолчанию | Рекомендация | Когда менять | Метрика melisai |
|-----------|-------------|-------------|--------------|-----------------|
| Планировщик I/O (SSD) | варьируется | **mq-deadline** | SSD выигрывают от простого планирования по deadline вместо сложных алгоритмов | `disk_avg_latency`, `biolatency_p99_ssd` |
| Планировщик I/O (HDD) | варьируется | **bfq** | Вращающиеся диски выигрывают от справедливости BFQ и гарантий задержки | `disk_avg_latency`, `biolatency_p99_hdd` |
| `read_ahead_kb` | 128 | **Зависит от нагрузки** | Увеличить для последовательного чтения (сканы баз данных); уменьшить для случайного I/O | `disk_utilization` |

**Изменение планировщика I/O:**
```bash
# Проверить текущий планировщик (активный в скобках):
cat /sys/block/sda/queue/scheduler
# [mq-deadline] none

# Изменить на лету:
echo mq-deadline > /sys/block/sda/queue/scheduler

# Сохранить через правило udev:
# /etc/udev/rules.d/60-scheduler.rules
# ACTION=="add|change", KERNEL=="sd*", ATTR{queue/rotational}=="0", ATTR{queue/scheduler}="mq-deadline"
# ACTION=="add|change", KERNEL=="sd*", ATTR{queue/rotational}=="1", ATTR{queue/scheduler}="bfq"
```

**Изменение read-ahead:**
```bash
# Проверить текущее значение:
cat /sys/block/sda/queue/read_ahead_kb

# Установить 256 КБ для последовательной нагрузки:
echo 256 > /sys/block/sda/queue/read_ahead_kb
```

---

## Проверка после настройки

После применения изменений запустите melisai для проверки результата:

```bash
# До настройки -- зафиксировать базовый уровень:
sudo melisai --duration 30s -o before.json

# Применить настройки (см. скрипт ниже)

# После настройки -- зафиксировать для сравнения:
sudo melisai --duration 30s -o after.json

# Сравнить отчёты:
melisai diff before.json after.json
```

На что обратить внимание в diff:
- Показатель здоровья должен увеличиться
- Ранее сработавшие аномалии должны исчезнуть
- Не должно появиться новых аномалий, вызванных изменениями

---

## Скрипт настройки одной командой

Следующий скрипт применяет все рекомендуемые настройки для production. Просмотрите и скорректируйте значения под вашу нагрузку перед запуском.

```bash
#!/usr/bin/env bash
# Настройка production от melisai -- применить: sudo bash tune.sh
# Создано для melisai v0.4.1
set -euo pipefail

SYSCTL_CONF="/etc/sysctl.d/99-melisai.conf"

cat > "$SYSCTL_CONF" << 'SYSCTL'
# === Планировщик CPU ===
kernel.sched_latency_ns = 6000000
kernel.sched_min_granularity_ns = 750000
kernel.sched_numa_balancing = 1

# === Память ===
vm.swappiness = 10
vm.dirty_ratio = 10
vm.dirty_background_ratio = 5
vm.overcommit_memory = 2
vm.min_free_kbytes = 131072
vm.watermark_scale_factor = 200
vm.dirty_expire_centisecs = 1500
vm.dirty_writeback_centisecs = 300
vm.zone_reclaim_mode = 0

# === Сеть: стек TCP ===
net.ipv4.tcp_congestion_control = bbr
net.core.default_qdisc = fq
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 8192
net.ipv4.tcp_rmem = 4096 131072 16777216
net.ipv4.tcp_wmem = 4096 131072 16777216
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_fastopen = 3
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_notsent_lowat = 131072
net.ipv4.tcp_mtu_probing = 1
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_intvl = 15
net.ipv4.tcp_keepalive_probes = 5

# === Сеть: обработка пакетов ===
net.core.netdev_budget = 600
net.core.netdev_budget_usecs = 8000
net.core.netdev_max_backlog = 10000

# === Сеть: таблица соседей (K8s / большие подсети) ===
net.ipv4.neigh.default.gc_thresh1 = 2048
net.ipv4.neigh.default.gc_thresh2 = 4096
net.ipv4.neigh.default.gc_thresh3 = 8192
SYSCTL

echo "[1/5] Записана конфигурация sysctl в $SYSCTL_CONF"

# Применить настройки sysctl
sysctl -p "$SYSCTL_CONF"
echo "[2/5] Применены настройки sysctl"

# THP: режим madvise
echo madvise > /sys/kernel/mm/transparent_hugepage/enabled
echo "defer+madvise" > /sys/kernel/mm/transparent_hugepage/defrag
echo "[3/5] Установлен THP в madvise, defrag в defer+madvise"

# Настройка NIC: максимальные кольцевые буферы и offload для всех физических интерфейсов
for iface in /sys/class/net/*/device; do
    iface_name=$(basename "$(dirname "$iface")")
    rx_max=$(ethtool -g "$iface_name" 2>/dev/null | awk '/Pre-set maximums/,/Current/ { if (/RX:/) print $2 }' | head -1)
    if [[ -n "$rx_max" && "$rx_max" != "n/a" ]]; then
        ethtool -G "$iface_name" rx "$rx_max" 2>/dev/null || true
    fi
    ethtool -K "$iface_name" tso on gro on 2>/dev/null || true
done
echo "[4/5] Настроены кольцевые буферы NIC и offload"

# Планировщик I/O: mq-deadline для SSD, bfq для HDD
for dev in /sys/block/sd* /sys/block/nvme*; do
    [[ -f "$dev/queue/rotational" ]] || continue
    rot=$(cat "$dev/queue/rotational")
    if [[ "$rot" == "0" ]]; then
        echo mq-deadline > "$dev/queue/scheduler" 2>/dev/null || true
    else
        echo bfq > "$dev/queue/scheduler" 2>/dev/null || true
    fi
done
echo "[5/5] Установлены планировщики I/O (mq-deadline для SSD, bfq для HDD)"

echo ""
echo "Настройка завершена. Запустите 'sudo melisai --duration 30s' для проверки."
echo "ВНИМАНИЕ: установлен vm.overcommit_memory=2. Убедитесь что overcommit_ratio настроен правильно."
echo "ВНИМАНИЕ: Проверьте conntrack_max и tcp_mem отдельно -- они зависят от вашей нагрузки."
```

### Что скрипт НЕ устанавливает

Следующие параметры требуют значений, специфичных для нагрузки, и намеренно опущены:

| Параметр | Почему опущен |
|----------|---------------|
| `net.netfilter.nf_conntrack_max` | Зависит от текущего использования; melisai рекомендует удвоить при >70% |
| `net.ipv4.tcp_mem` | Автовычисляется ядром на основе RAM; увеличивать только если `PruneCalled` > 0 |
| `vm.overcommit_ratio` | Требуется при `overcommit_memory=2`; установите 80-95% в зависимости от нагрузки |
| `read_ahead_kb` | Зависит от паттерна нагрузки (последовательный vs. случайный) |

---

## Краткий справочник: соответствие аномалий melisai и sysctl

Когда melisai фиксирует аномалию, эта таблица показывает, какой параметр настраивать в первую очередь:

| Аномалия melisai | Первый параметр для проверки |
|------------------|------------------------------|
| `runqlat_p99` высокий | `kernel.sched_latency_ns`, `kernel.sched_min_granularity_ns` |
| `swap_usage` предупреждение | `vm.swappiness`, проверить утечку памяти |
| `memory_psi_pressure` | `vm.min_free_kbytes`, `vm.watermark_scale_factor`, настройки THP |
| `tcp_retransmits` высокий | `net.ipv4.tcp_congestion_control=bbr`, размеры буферов |
| `tcp_timewait` высокий | `net.ipv4.tcp_tw_reuse`, `ip_local_port_range`, `tcp_fin_timeout` |
| `listen_overflows` | `net.core.somaxconn`, `tcp_max_syn_backlog` |
| `conntrack_usage_pct` высокий | `net.netfilter.nf_conntrack_max` |
| `softnet_dropped` | `net.core.netdev_max_backlog` |
| `softnet_time_squeeze` | `net.core.netdev_budget`, `netdev_budget_usecs` |
| `nic_rx_discards` | `ethtool -G <iface> rx <max>` |
| `tcp_close_wait` | `tcp_keepalive_time/intvl/probes` (и исправить приложение) |
| `tcp_abort_on_memory` | `net.ipv4.tcp_mem` |
| `irq_imbalance` | сервис `irqbalance` или ручная настройка `smp_affinity` |
| `udp_rcvbuf_errors` | `net.core.rmem_max`, `SO_RCVBUF` в приложении |
| `disk_avg_latency` высокий | Планировщик I/O, `read_ahead_kb` |
| `cpu_utilization` критический | Профилирование с `perf`/`flamegraph`; настройка планировщика вторична |

---

## Чек-лист сохранения настроек

После применения настроек убедитесь, что эти пункты переживут перезагрузку:

- [ ] `/etc/sysctl.d/99-melisai.conf` существует и содержит ваши настройки
- [ ] `sysctl -p /etc/sysctl.d/99-melisai.conf` выполняется без ошибок
- [ ] Настройки THP в `/etc/rc.local` или systemd-юните (sysfs не покрывается sysctl)
- [ ] Кольцевые буферы NIC в `/etc/udev/rules.d/` или скрипте networkd/ifup
- [ ] Планировщик I/O в `/etc/udev/rules.d/60-scheduler.rules`
- [ ] `irqbalance` запущен: `systemctl enable --now irqbalance`

---

*Этот чек-лист покрывает все параметры, которые melisai v0.4.1 собирает и анализирует.*
