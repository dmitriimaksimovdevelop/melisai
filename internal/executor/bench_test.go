package executor

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// --- Histogram Benchmarks ---

func BenchmarkParseHistogram(b *testing.B) {
	raw := readBenchdata(b, "biolatency.txt")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseHistogram(raw, "block_io_latency", "us")
	}
}

func BenchmarkParsePerDiskHistogram(b *testing.B) {
	raw := readBenchdata(b, "biolatency_per_disk.txt")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParsePerDiskHistogram(raw, "us")
	}
}

// --- Event Benchmarks ---

func BenchmarkParseTabularEvents(b *testing.B) {
	raw := readBenchdata(b, "tcpconnlat.txt")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseTabularEvents(raw, 1000)
	}
}

func BenchmarkParseTabularEventsLarge(b *testing.B) {
	// Simulate a large tabular output with 1000 events
	var sb strings.Builder
	sb.WriteString("PID COMM DADDR DPORT LAT(ms)\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&sb, "%d curl 10.0.0.%d 443 %.1f\n", 1000+i, i%256, float64(i)*0.1)
	}
	raw := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseTabularEvents(raw, 1000)
	}
}

// --- Stack Benchmarks ---

func BenchmarkParseFoldedStacks(b *testing.B) {
	raw := readBenchdata(b, "profile_folded.txt")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseFoldedStacks(raw, "on-cpu")
	}
}

func BenchmarkParseFoldedStacksLarge(b *testing.B) {
	// Simulate 500 unique stack traces
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&sb, "main;func_%d;subfunc_%d %d\n", i%50, i%10, 100+i)
	}
	raw := sb.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseFoldedStacks(raw, "on-cpu")
	}
}

// --- Aggregation Benchmarks ---

func BenchmarkAggregateByField(b *testing.B) {
	events := make([]model.Event, 1000)
	for i := range events {
		events[i] = model.Event{
			Details: map[string]interface{}{
				"raddr": fmt.Sprintf("10.0.0.%d", i%50),
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = AggregateByField(events, "raddr", 10)
	}
}

func BenchmarkAggregateConnections(b *testing.B) {
	events := make([]model.Event, 1000)
	for i := range events {
		events[i] = model.Event{
			Details: map[string]interface{}{
				"daddr":   fmt.Sprintf("10.0.0.%d", i%50),
				"lat(ms)": float64(i%100) * 0.5,
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = AggregateConnections(events)
	}
}

// --- FormatKey Benchmark ---

func BenchmarkFormatKey(b *testing.B) {
	vals := []interface{}{"10.0.0.1", 42.5, 1234, int64(9999)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = formatKey(vals[i%len(vals)])
	}
}

// readBenchdata reads testdata for benchmarks.
func readBenchdata(b *testing.B, name string) string {
	b.Helper()
	path := testdataPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatalf("read testdata %s: %v", name, err)
	}
	return string(data)
}
