# Глава 16: Форматы вывода

## JSON

- Атомарная запись через временный файл + rename
- `json.MarshalIndent` с отступами для читаемости

## Flame Graph (SVG)

Визуализация CPU-профиля: ширина = доля CPU-времени, чтение снизу вверх (вызывающий → вызываемый).

## Прогресс

```
sysdiag v0.2.0 — сбор с профилем standard (30с)
  cpu_utilization               ✓ 1.003s
  memory_info                   ✓ 12ms
  disk_io                       ✓ 1.001s
  network_stats                 ✓ 45ms
  process_top                   ✓ 1.005s
  container_info                ✓ 3ms
  system_info                   ✓ 218ms
```

---

*Далее: [Глава 17 — Приложение](17-appendix.md)*
