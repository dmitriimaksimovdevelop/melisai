# Быстрый старт

От нуля до диагноза за 2 минуты.

## 1. Установка

```bash
# Один команда (Linux amd64/arm64)
curl -sSL https://melisai.dev/install | sh

# Или сборка из исходников
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o melisai ./cmd/melisai/
sudo mv melisai /usr/local/bin/
```

## 2. Первый запуск

```bash
# Быстрая проверка (10 секунд, BCC tools не обязательны)
sudo melisai collect --profile quick -o report.json
```

Вы увидите:

```
melisai v0.4.1 | profile=quick | duration=10s

Tier 1 (procfs)  ████████████████████████████████████████ 8/8   2.1s
Tier 2 (BCC)     ████████████████████████████████████████ 4/4  10.3s

Health Score:  68 / 100  ⚠️
Anomalies:     cpu_utilization CRITICAL (98.7%)
               load_average WARNING (3.2x CPUs)
Recommendations: 2

Report saved to report.json
```

## 3. Чтение результатов

```bash
# Оценка здоровья (0-100)
jq '.summary.health_score' report.json

# Что не так?
jq '.summary.anomalies[]' report.json

# Как починить
jq '.summary.recommendations[] | {type, title, commands}' report.json
```

## 4. Применение исправлений

Рекомендации содержат готовые команды:

```bash
# Пример: melisai рекомендует включить BBR
sysctl -w net.core.default_qdisc=fq
sysctl -w net.ipv4.tcp_congestion_control=bbr
```

## 5. Проверка результата

```bash
# Запустить снова и сравнить
sudo melisai collect --profile quick -o after.json
melisai diff report.json after.json
```

Diff покажет что улучшилось, что ухудшилось, и дельту health score.

## Что дальше?

| Цель | Команда |
|------|---------|
| Полный анализ (все 67 BCC tools) | `sudo melisai collect --profile standard -o report.json` |
| Глубокий анализ (стеки, memleak) | `sudo melisai collect --profile deep -o report.json` |
| Только сеть | `sudo melisai collect --profile standard --focus network -o net.json` |
| Конкретный процесс | `sudo melisai collect --profile standard --pid 12345 -o app.json` |
| Контейнер | `sudo melisai collect --profile standard --cgroup /sys/fs/cgroup/.../nginx.service -o cg.json` |
| Установить BCC tools | `sudo melisai install` |
| Использовать с Claude/Cursor | `melisai mcp` → [гайд по MCP](13-ai-integration.md) |

## Профили сбора

| Профиль | Время | Что запускается | Для чего |
|---------|-------|-----------------|----------|
| **quick** | 10с | Tier 1 + 4 основных BCC | Проверка здоровья, CI |
| **standard** | 30с | Все Tier 1 + все 67 BCC | Обычная диагностика |
| **deep** | 60с | Всё + memleak, biostacks | Поиск корневой причины |

## Справка по CLI

```
melisai collect   Сбор метрик и генерация JSON-отчёта
  --profile       quick|standard|deep (по умолчанию: standard)
  --focus         cpu,memory,disk,network,stacks
  --pid           Фильтр по PID
  --cgroup        Фильтр по cgroup
  --duration      Переопределить длительность (например, 15s, 1m)
  --ai-prompt     Включить AI-промпт в отчёт
  --output, -o    Файл вывода (- для stdout)

melisai diff      Сравнение двух отчётов
melisai install   Установка BCC tools
melisai mcp       MCP сервер (stdio JSON-RPC)
melisai capabilities  Доступные инструменты и возможности ядра
```

---

*Далее: [Введение — Как работает melisai](00-introduction.md)*
