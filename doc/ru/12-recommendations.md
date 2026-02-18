# Глава 12: Движок рекомендаций

## Обзор

После обнаружения аномалий melisai предлагает конкретные команды `sysctl` для исправления. Рекомендации генерируются на основе собранных метрик.

## Примеры рекомендаций

### Сеть
- **Включить TCP BBR** когда `tcp_congestion_control = cubic`
- **Увеличить somaxconn** когда < 4096
- **Увеличить TCP буферы** для WAN с высоким RTT

### Память
- **Снизить swappiness** для БД (`vm.swappiness=1`)
- **Уменьшить dirty_ratio** для быстрой записи

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
