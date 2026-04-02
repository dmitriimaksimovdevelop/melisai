# Глава 18: Анализ GPU и топологии PCIe

## Обзор

Задание GPU-вычислений, которое должно утилизировать 400 Gbps InfiniBand, еле ползёт на 60% пропускной способности. GPU в порядке. NIC в порядке. Проблема: GPU расположен на NUMA-ноде 0, а NIC на NUMA-ноде 1. Каждый DMA-трансфер проходит через межсокетную шину. Штраф 30-50% пропускной способности, невидимый для метрик уровня приложения.

`GPUCollector` в melisai (`internal/collector/gpu.go`) обнаруживает это автоматически. Он опрашивает NVIDIA GPU через `nvidia-smi`, сопоставляет PCI-устройства и NIC с NUMA-нодами через sysfs и помечает каждую пару GPU-NIC, пересекающую границу NUMA.

## Исходный файл: gpu.go

- **Строк**: 166
- **Функций**: 7
- **Уровень**: 1 (root не требуется, sysfs доступен на чтение всем)
- **Категория**: `system`
- **Имя коллектора**: `gpu_pcie`

## Почему топология PCIe важна

Современные серверы имеют несколько корневых комплексов PCIe, по одному на каждый процессорный сокет. Каждый корневой комплекс владеет набором PCIe-слотов. Устройства в этих слотах имеют локальный доступ к контроллеру памяти данного сокета -- это и есть NUMA-нода устройства.

Когда GPU на NUMA-ноде 0 отправляет данные через DMA на NIC на NUMA-ноде 1, трансфер проходит через межсокетную шину (UPI на Intel, Infinity Fabric на AMD):

| Сценарий | Влияние на пропускную способность | Влияние на задержку |
|----------|----------------------------------|---------------------|
| Та же NUMA-нода | Базовый уровень | Базовый уровень |
| Cross-NUMA (2 сокета) | Снижение на 30-50% | +40-80 нс на доступ |
| Cross-NUMA (4 сокета) | Снижение до 70% | +100-200 нс на хоп |

Ядро вас не предупредит. `nvidia-smi` вас не предупредит. Приложения видят низкую пропускную способность и обвиняют сеть. melisai ловит это.

## Структуры данных

Три типа в `internal/model/types.go`:

```go
type GPUDevice struct {
    Index       int    `json:"index"`
    Name        string `json:"name"`
    Driver      string `json:"driver,omitempty"`
    PCIBus      string `json:"pci_bus"`
    NUMANode    int    `json:"numa_node"`
    MemoryTotal int64  `json:"memory_total_mb,omitempty"`
    MemoryUsed  int64  `json:"memory_used_mb,omitempty"`
    UtilGPU     int    `json:"utilization_gpu_pct,omitempty"`
    UtilMemory  int    `json:"utilization_memory_pct,omitempty"`
    Temperature int    `json:"temperature_c,omitempty"`
    PowerWatts  int    `json:"power_watts,omitempty"`
}

type PCIeTopology struct {
    GPUs           []GPUDevice     `json:"gpus,omitempty"`
    NICNUMAMap     map[string]int  `json:"nic_numa_map,omitempty"`
    CrossNUMAPairs []CrossNUMAPair `json:"cross_numa_pairs,omitempty"`
}

type CrossNUMAPair struct {
    GPU     string `json:"gpu"`
    GPUNode int    `json:"gpu_numa_node"`
    NIC     string `json:"nic"`
    NICNode int    `json:"nic_numa_node"`
}
```

## Как работает обнаружение

`Collect()` выполняет три шага:

```go
func (c *GPUCollector) Collect(ctx context.Context, cfg CollectConfig) (*model.Result, error) {
    topo := &model.PCIeTopology{NICNUMAMap: make(map[string]int)}
    topo.GPUs = c.detectNvidiaGPUs(ctx)     // Шаг 1
    c.buildNICNUMAMap(topo)                  // Шаг 2
    c.findCrossNUMAPairs(topo)               // Шаг 3

    if len(topo.GPUs) == 0 && len(topo.NICNUMAMap) == 0 {
        return nil, nil   // корректно: ничего не обнаружено, нет результата
    }
    return &model.Result{Collector: c.Name(), Data: topo}, nil
}
```

Возврат `nil, nil` означает "не применимо". Оркестратор исключает этот коллектор из отчёта. Без лишнего шума.

### Шаг 1: detectNvidiaGPUs

Запускает nvidia-smi со структурированным CSV-выводом:

```
nvidia-smi --query-gpu=index,name,driver_version,pci.bus_id,memory.total,\
  memory.used,utilization.gpu,utilization.memory,temperature.gpu,power.draw \
  --format=csv,noheader,nounits
```

- **Таймаут 5 секунд** -- nvidia-smi может зависнуть при проблемном драйвере. Выделенный `context.WithTimeout` предотвращает блокировку сбора данных.
- **Корректная деградация** -- если nvidia-smi отсутствует или завершается с ошибкой, возвращает nil.
- **Поиск NUMA** -- для каждого GPU читает `/sys/bus/pci/devices/<bus_id>/numa_node`. PCI bus ID из nvidia-smi (например, `00000000:07:00.0`) напрямую отображается на путь в sysfs.

### Шаг 2: buildNICNUMAMap

Читает `/sys/class/net/*/device/numa_node` для каждого физического NIC. Отфильтровывает виртуальные интерфейсы: `lo`, `veth*`, `docker*`, `br-*`. Значение NUMA-ноды `-1` (одно-сокетная система или виртуальное устройство) пропускается.

### Шаг 3: findCrossNUMAPairs

Декартово произведение: для каждого GPU проверяется каждый NIC. Если они находятся на разных NUMA-нодах и оба имеют корректное назначение (>= 0), пара фиксируется:

```go
if gpu.NUMANode != nicNode && gpu.NUMANode >= 0 && nicNode >= 0 {
    topo.CrossNUMAPairs = append(topo.CrossNUMAPairs, model.CrossNUMAPair{
        GPU: gpu.Name, GPUNode: gpu.NUMANode,
        NIC: nic,      NICNode: nicNode,
    })
}
```

## Обнаружение аномалий

Правило `gpu_nic_cross_numa` в `internal/model/anomaly.go`:

```go
{
    Metric: "gpu_nic_cross_numa", Category: "system",
    Warning: 1, Critical: 1,
    Evaluator: func(r *Report) (float64, bool) {
        // Сканирует категорию system на наличие данных PCIeTopology
        // Возвращает количество cross-NUMA пар
        return float64(len(topo.CrossNUMAPairs)), true
    },
    Message: func(v float64) string {
        return fmt.Sprintf(
            "GPU-NIC cross-NUMA: %.0f pair(s) on different NUMA nodes (PCIe DMA penalty)", v)
    },
},
```

Warning=1, Critical=1: cross-NUMA -- это бинарное состояние. Оно либо есть, либо нет. Одна неправильно расположенная пара может снизить пропускную способность на 30-50%, поэтому даже одна пара является критической для GPU-нагрузок.

## Примеры JSON-вывода

### Здоровая конфигурация: GPU и NIC на одной NUMA-ноде

```json
{
  "collector": "gpu_pcie",
  "category": "system",
  "tier": 1,
  "data": {
    "gpus": [
      {
        "index": 0,
        "name": "NVIDIA A100-SXM4-80GB",
        "driver": "535.129.03",
        "pci_bus": "00000000:07:00.0",
        "numa_node": 0,
        "memory_total_mb": 81920,
        "memory_used_mb": 42317,
        "utilization_gpu_pct": 87,
        "temperature_c": 62,
        "power_watts": 312
      }
    ],
    "nic_numa_map": {
      "eth0": 0,
      "ib0": 0
    }
  }
}
```

Поле `cross_numa_pairs` отсутствует -- исключено через `omitempty`, так как срез равен nil.

### Проблема: Cross-NUMA пара GPU-NIC

```json
{
  "collector": "gpu_pcie",
  "category": "system",
  "tier": 1,
  "data": {
    "gpus": [
      {"index": 0, "name": "NVIDIA A100-SXM4-80GB",
       "pci_bus": "00000000:07:00.0", "numa_node": 0},
      {"index": 1, "name": "NVIDIA A100-SXM4-80GB",
       "pci_bus": "00000000:8A:00.0", "numa_node": 1}
    ],
    "nic_numa_map": {"ib0": 1, "eth0": 0},
    "cross_numa_pairs": [
      {"gpu": "NVIDIA A100-SXM4-80GB", "gpu_numa_node": 0,
       "nic": "ib0", "nic_numa_node": 1},
      {"gpu": "NVIDIA A100-SXM4-80GB", "gpu_numa_node": 1,
       "nic": "eth0", "nic_numa_node": 0}
    ]
  }
}
```

Аномалия срабатывает:

```json
{
  "metric": "gpu_nic_cross_numa",
  "value": 2,
  "threshold": 1,
  "severity": "critical",
  "message": "GPU-NIC cross-NUMA: 2 pair(s) on different NUMA nodes (PCIe DMA penalty)"
}
```

### GPU не обнаружен

`detectNvidiaGPUs()` возвращает nil. Если у NIC тоже нет NUMA-привязки, `Collect()` возвращает `nil, nil`. Коллектор полностью отсутствует в отчёте.

## Диагностические команды

### nvidia-smi topo

```bash
$ nvidia-smi topo -m
        GPU0    GPU1    mlx5_0  mlx5_1  CPU Affinity    NUMA Affinity
GPU0     X      NV12    SYS     PHB     0-19            0
GPU1    NV12     X      PHB     SYS     20-39           1
mlx5_0  SYS     PHB      X      SYS    20-39           1
mlx5_1  PHB     SYS     SYS      X     0-19            0
```

- **PHB** = тот же PCIe Host Bridge (та же NUMA-нода)
- **SYS** = пересекает границу NUMA (межсокетная шина)
- **NV12** = NVLink (GPU-to-GPU)

GPU0-mlx5_0 обозначен как SYS (cross-NUMA) -- именно то, что обнаруживает melisai.

### Прямая проверка через sysfs

```bash
$ cat /sys/bus/pci/devices/0000:07:00.0/numa_node    # GPU
0
$ cat /sys/class/net/ib0/device/numa_node             # NIC
1
# Cross-NUMA подтверждён
```

### numactl

```bash
$ numactl --hardware
available: 2 nodes (0-1)
node 0 cpus: 0-19
node 1 cpus: 20-39
node distances:
node   0   1
  0:  10  21
  1:  21  10
```

Дистанция 21 против локальных 10 количественно характеризует штраф.

## Исправление проблем cross-NUMA

### Вариант 1: Физическое перемещение в другой слот

Переместите GPU или NIC в PCIe-слот на том же сокете. Единственное решение, полностью устраняющее штраф.

```bash
# Какие слоты на какой NUMA-ноде
$ for dev in /sys/bus/pci/devices/*/numa_node; do
    echo "$(dirname $dev | xargs basename): $(cat $dev)"
  done | sort -t: -k2 -n
```

### Вариант 2: Привязка через numactl

Привяжите приложение к NUMA-ноде GPU. Не устраняет пересечение NIC, но сохраняет CPU и память локальными относительно GPU:

```bash
$ CUDA_VISIBLE_DEVICES=0 numactl --cpunodebind=0 --membind=0 ./train.py
```

### Вариант 3: Выбор правильного NIC

Направьте трафик GPU через NIC на той же NUMA-ноде:

```bash
$ cat /sys/class/net/ib0/device/numa_node   # 1
$ cat /sys/class/net/ib1/device/numa_node   # 0 -- используйте этот для GPU0
$ ip route add 10.0.0.0/24 dev ib1
```

Для мульти-GPU обучения с NCCL:

```bash
$ export NCCL_SOCKET_IFNAME=ib1
$ export NCCL_IB_HCA=mlx5_1   # HCA на той же NUMA-ноде что и GPU
```

### Вариант 4: Привязка IRQ

Закрепите прерывания NIC на CPU, принадлежащих NUMA-ноде GPU:

```bash
$ cat /proc/interrupts | grep mlx5
$ echo 000fffff > /proc/irq/<irq_num>/smp_affinity
```

## GPUDirect RDMA

GPUDirect RDMA позволяет NIC выполнять DMA напрямую в/из памяти GPU, минуя оперативную память хоста. Чрезвычайно чувствителен к топологии PCIe:

1. **Та же NUMA-нода** -- полная пропускная способность
2. **Cross-NUMA** -- работает, но со сниженной пропускной способностью (DMA всё ещё пересекает межсокетную шину)
3. **За PCIe-коммутатором** -- лучший случай, peer-to-peer остаётся внутри коммутатора

```bash
$ lsmod | grep nv_peer_mem                              # загружен?
$ NCCL_DEBUG=INFO ./my_app 2>&1 | grep -i "gpu direct"  # активен?
```

Обнаружение cross-NUMA в melisai особенно ценно здесь: неправильно настроенная топология превращает путь с нулевым копированием в двухэтапный DMA с худшей производительностью, чем обычные трансферы через хостовую память.

## Проектные решения

**Почему nvidia-smi, а не NVML?** Исключает CGO-зависимость от libnvidia-ml.so. Сохраняет статическую сборку. Работает даже когда заголовки NVML не соответствуют версии драйвера.

**Почему уровень 1?** sysfs доступен для чтения всем. nvidia-smi не требует root. Весь коллектор работает без привилегий.

**Почему возврат `nil, nil`?** Сервер без GPU не должен иметь секцию GPU с пустыми массивами. Nil означает "не применимо".

**Почему Warning=1 и Critical=1?** Не бывает "слегка cross-NUMA". Либо ваша топология правильная, либо нет.

## Краткий справочник

| Что | Где |
|-----|-----|
| Исходный код коллектора | `internal/collector/gpu.go` |
| Типы модели | `internal/model/types.go` (GPUDevice, PCIeTopology, CrossNUMAPair) |
| Правило аномалии | `internal/model/anomaly.go` (`gpu_nic_cross_numa`) |
| NUMA GPU в sysfs | `/sys/bus/pci/devices/<bus_id>/numa_node` |
| NUMA NIC в sysfs | `/sys/class/net/<iface>/device/numa_node` |
| Топология nvidia-smi | `nvidia-smi topo -m` |
| Информация о NUMA | `numactl --hardware` |
| Визуальная топология | `lstopo` (из пакета hwloc) |

---

*Далее: [Глава 19 -- Page Reclaim и THP](19-page-reclaim-thp.md)*
