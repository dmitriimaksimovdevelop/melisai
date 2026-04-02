# Глава 12: Движок рекомендаций

## Обзор

После обнаружения аномалий melisai предлагает конкретные команды для исправления. Рекомендации генерируются на основе собранных метрик.

## Примеры рекомендаций

### Сеть
- **Включить TCP BBR** когда `tcp_congestion_control != bbr`
- **Увеличить somaxconn** когда < 4096
- **Увеличить TCP буферы** для WAN с высоким RTT
- **Conntrack** — удвоить `nf_conntrack_max` когда заполнение > 70%
- **Ring buffer** — увеличить до максимума при rx_discards > 0
- **Listen overflows** — увеличить somaxconn, добавить SO_REUSEPORT
- **TCP memory pressure** — увеличить `tcp_mem` при PruneCalled > 0
- **TIME_WAIT reuse** — включить `tcp_tw_reuse` при > 1000 TIME_WAIT
- **netdev_max_backlog** — увеличить при softnet дропах
- **rmem_max/wmem_max** — увеличить до 16MB при < 16MB
- **ip_local_port_range** — расширить до `1024 65535`
- **tcp_slow_start_after_idle** — отключить для persistent connections
- **tcp_fastopen** — включить (client+server, значение 3)
- **netdev_budget** — увеличить при time_squeeze
- **UDP rmem_max** — увеличить при RcvbufErrors

### Память
- **Снизить swappiness** для БД (`vm.swappiness=10`)
- **Уменьшить dirty_ratio** для быстрой записи
- **Отключить THP** для latency-sensitive нагрузок
- **Увеличить min_free_kbytes** на системах > 16 GiB

### Диск
- **mq-deadline** для SSD
- **BFQ** для HDD

### Персистентность

```bash
# Временно (немедленный эффект):
sysctl -w net.ipv4.tcp_congestion_control=bbr

# Постоянно (сохраняется после перезагрузки):
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.d/99-melisai.conf
sysctl -p /etc/sysctl.d/99-melisai.conf
```

---

*Далее: [Глава 13 — AI-интеграция](13-ai-integration.md)*
