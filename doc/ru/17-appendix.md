# Глава 17: Приложение

## Глоссарий

| Термин | Определение |
|--------|-----------|
| **BCC** | BPF Compiler Collection — eBPF-инструменты на Python |
| **eBPF** | Extended BPF — виртуальная машина в ядре для трассировки |
| **BTF** | BPF Type Format — метаданные структур ядра |
| **CO-RE** | Compile Once, Run Everywhere — портируемые eBPF-программы |
| **CFS** | Completely Fair Scheduler — планировщик CPU |
| **cgroup** | Control Group — ограничение ресурсов |
| **jiffy** | Один тик (1/HZ секунды) |
| **NUMA** | Non-Uniform Memory Access |
| **OOM** | Out of Memory — ядро убивает процесс |
| **PSI** | Pressure Stall Information — метрика конкуренции за ресурсы |
| **RSS** | Resident Set Size — физическая память процесса |
| **USE** | Utilization, Saturation, Errors — методология Грегга |

## Справка CLI

```
sysdiag collect [флаги]
  --profile string    quick|standard|deep (по умолчанию "standard")
  --focus string      Области фокуса: cpu,disk,network,stacks,all
  --output string     Путь к файлу (по умолчанию: stdout)
  --ai-prompt         Включить промпт для AI
  --quiet             Без прогресса

sysdiag diff <baseline.json> <current.json> [--json]
sysdiag capabilities
sudo sysdiag install
```

## Литература

1. Gregg, Brendan. **"Systems Performance"**, 2nd Edition. 2020.
2. Gregg, Brendan. **"BPF Performance Tools"**. 2019.
3. Документация ядра Linux: https://www.kernel.org/doc/
4. BCC: https://github.com/iovisor/bcc
5. cilium/ebpf: https://github.com/cilium/ebpf
6. Блог Брендана Грегга: https://www.brendangregg.com/
