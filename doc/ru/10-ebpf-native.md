# Глава 10: Нативный eBPF

Tier 3 — будущее наблюдаемости Linux: загрузка eBPF-программ напрямую из Go-кода.

## BTF и CO-RE

- **BTF** (BPF Type Format) — метаданные о структурах ядра
- **CO-RE** (Compile Once, Run Everywhere) — единожды скомпилированная программа работает на разных ядрах

## Загрузчик (Реализация)

Файл `loader.go` использует библиотеку `cilium/ebpf` для загрузки и подключения программ:

```go
type Loader struct {
    btfInfo *BTFInfo
    verbose bool
}

func (l *Loader) TryLoad(ctx context.Context, spec *ProgramSpec) (*LoadedProgram, error) {
    // 1. Загрузка скомпилированного BPF-объекта (.o файл)
    collSpec, err := ebpf.LoadCollectionSpec(spec.ObjectFile)

    // 2. Загрузка в ядро (верификация и JIT)
    // На этом этапе автоматически выполняются CO-RE перемещения
    coll, err := ebpf.NewCollection(collSpec)

    // 3. Подключение kprobe
    kp, err := link.Kprobe(spec.AttachTo, prog, nil)

    return &LoadedProgram{Collection: coll, Link: kp}, nil
}
```

Система требует наличия `.o` файлов (скомпилированный байт-код eBPF). В будущем они могут быть встроены в бинарник с помощью директивы `//go:embed`.

### Нативные коллекторы

`NativeTcpretransCollector` (`internal/collector/ebpf_tcpretrans.go`) демонстрирует использование:

1.  **Чтение Perf Buffer**: Использует `perf.NewReader` для чтения событий из BPF-карты.
2.  **Парсинг бинарных данных**: Использует `binary.Read` для конвертации байтов в Go-структуры.
3.  **Без оверхеда на переключение контекста**: В отличие от BCC (Python -> C -> Go), здесь чистый путь Go -> Kernel.

| Свойство | Tier 2 (BCC) | Tier 3 (Native) |
|---------|-------------|-----------------|
| Зависимости | Python, bcc-tools | Нет (встроено в бинарник) |
| Время запуска | 1-3 сек на инструмент | < 100мс |
| Память | 50-100МБ (Python) | < 5МБ |

---

*Далее: [Глава 11 — Обнаружение аномалий](11-anomaly-detection.md)*
