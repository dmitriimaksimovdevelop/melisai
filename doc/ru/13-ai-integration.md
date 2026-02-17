# Глава 13: AI-интеграция

sysdiag генерирует промпт с 21 антипаттерном (P1-P21) для LLM-анализа. Антипаттерны фильтруются на основе собранных данных.

## Ключевые антипаттерны

| ID | Антипаттерн | Когда срабатывает |
|----|------------|------------------|
| P1 | Насыщение одного CPU | Одно ядро на 99%+ |
| P5 | Container throttling | nr_throttled > 0 |
| P6 | Утечка CLOSE_WAIT | close_wait_count > 0 |
| P13 | Cubic на WAN | cubic + RetransSegs > 1% |
| P17 | Steal на VM | steal > 5% |
| P20 | OOM в dmesg | "Out of memory" |

---

*Далее: [Глава 14 — Сравнение отчётов](14-report-diffing.md)*
