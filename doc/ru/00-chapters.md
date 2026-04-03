# Все главы

Полное оглавление документации melisai. 22 главы — от теории производительности Linux до production-тюнинга.

## Начало работы

| # | Глава | Что узнаете |
|---|-------|-------------|
| — | [Быстрый старт](00-quickstart.md) | Установка → запуск → чтение → исправление → проверка. 2 минуты |
| 0 | [Введение](index.md) | Что такое melisai, методология USE, трёхуровневая архитектура |
| 1 | [Основы Linux](01-linux-fundamentals.md) | /proc, /sys, jiffies, состояния CPU, cgroups, PSI, buddy allocator |

## Анализ производительности

| # | Глава | Что узнаете |
|---|-------|-------------|
| 2 | [Анализ CPU](02-cpu-analysis.md) | Дельта-сэмплинг, per-CPU, load average, настройка CFS |
| 3 | [Анализ памяти](03-memory-analysis.md) | MemAvailable, vmstat, PSI, NUMA, swap, dirty pages |
| 4 | [Анализ дисков](04-disk-analysis.md) | /proc/diskstats, секторы 512 байт, планировщики I/O |
| 5 | [Анализ сети](05-network-analysis.md) | TCP, conntrack, softnet, IRQ, NIC hardware, 30+ sysctls |
| 6 | [Анализ процессов](06-process-analysis.md) | Top-N по CPU/памяти, /proc/[pid]/stat, FD, состояния |
| 7 | [Анализ контейнеров](07-container-analysis.md) | K8s/Docker, cgroup v1/v2, CPU throttling, лимиты памяти |

## Внутреннее устройство

| # | Глава | Что узнаете |
|---|-------|-------------|
| 8 | [Системный коллектор](08-system-collector.md) | ОС, uptime, файловые системы, блочные устройства, dmesg |
| 9 | [BCC инструменты](09-bcc-tools.md) | Реестр 67 инструментов, executor, безопасность, парсеры |
| 10 | [Нативный eBPF](10-ebpf-native.md) | BTF/CO-RE, cilium/ebpf loader, стратегия Tier 3 |
| 15 | [Оркестратор](15-orchestrator.md) | Двухфазный сбор, параллельные коллекторы, профили |
| 16 | [Форматы вывода](16-output-formats.md) | JSON-схема, FlameGraph SVG, progress reporter |

## Интеллект

| # | Глава | Что узнаете |
|---|-------|-------------|
| 11 | [Обнаружение аномалий](11-anomaly-detection.md) | 37 пороговых правил, rate-based детекция, формула health score |
| 12 | [Рекомендации](12-recommendations.md) | 35 sysctl-исправлений, типы "fix" и "optimization" |
| 13 | [AI-интеграция](13-ai-integration.md) | Генерация промпта, 27 анти-паттернов, настройка MCP |

## Продвинутые темы

| # | Глава | Что узнаете |
|---|-------|-------------|
| 18 | [GPU и PCIe](18-gpu-pcie-analysis.md) | NVIDIA детекция, PCI→NUMA маппинг, cross-NUMA GPU-NIC, GPUDirect |
| 19 | [Page Reclaim и THP](19-page-reclaim-thp.md) | Watermarks, direct reclaim, компакция, режимы THP defrag |
| 20 | [Оптимизация NUMA](20-numa-optimization.md) | Матрица расстояний, miss ratio, numactl, K8s topology manager |

## Эксплуатация

| # | Глава | Что узнаете |
|---|-------|-------------|
| 14 | [Сравнение отчётов](14-report-diffing.md) | Before/after, дельты USE, изменения гистограмм |
| 21 | [Чек-лист для production](21-production-checklist.md) | Все sysctls, скрипт тюнинга, маппинг аномалия→исправление |

## Справочник

| # | Глава | Что узнаете |
|---|-------|-------------|
| 17 | [Приложение](17-appendix.md) | Глоссарий, справка /proc, /sys, таблица sysctl, CLI |
