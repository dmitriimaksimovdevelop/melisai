package executor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func testdataPath(name string) string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filename))), "testdata", name)
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(testdataPath(name))
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return string(data)
}

// --- Histogram Parser Tests ---

func TestParseHistogram(t *testing.T) {
	raw := readTestdata(t, "biolatency.txt")
	hist, err := ParseHistogram(raw, "block_io_latency", "us")
	if err != nil {
		t.Fatalf("ParseHistogram: %v", err)
	}

	if hist.Name != "block_io_latency" {
		t.Errorf("name = %q, want block_io_latency", hist.Name)
	}
	if hist.Unit != "us" {
		t.Errorf("unit = %q, want us", hist.Unit)
	}
	if len(hist.Buckets) == 0 {
		t.Fatal("no buckets parsed")
	}

	// Verify total count
	if hist.TotalCount == 0 {
		t.Error("totalCount = 0")
	}

	// Verify bucket ordering (low values first)
	for i := 1; i < len(hist.Buckets); i++ {
		if hist.Buckets[i].Low <= hist.Buckets[i-1].Low {
			t.Errorf("bucket ordering broken: [%d].Low=%d <= [%d].Low=%d",
				i, hist.Buckets[i].Low, i-1, hist.Buckets[i-1].Low)
		}
	}

	// P50 should be reasonable
	if hist.P50 < 4 || hist.P50 > 64 {
		t.Errorf("p50 = %v, expected 4-64 range", hist.P50)
	}
	// P99 should be higher than P50
	if hist.P99 <= hist.P50 {
		t.Errorf("p99 (%v) should be > p50 (%v)", hist.P99, hist.P50)
	}
	// Mean should be positive
	if hist.Mean <= 0 {
		t.Errorf("mean = %v, expected > 0", hist.Mean)
	}
}

func TestParseHistogramEmpty(t *testing.T) {
	_, err := ParseHistogram("no histogram here", "test", "us")
	if err == nil {
		t.Error("expected error for empty histogram")
	}
}

func TestParsePerDiskHistogram(t *testing.T) {
	raw := readTestdata(t, "biolatency_per_disk.txt")
	hists, err := ParsePerDiskHistogram(raw, "us")
	if err != nil {
		t.Fatalf("ParsePerDiskHistogram: %v", err)
	}

	if len(hists) != 2 {
		t.Fatalf("expected 2 disk histograms, got %d", len(hists))
	}

	// Verify disk names are in histogram names
	names := map[string]bool{}
	for _, h := range hists {
		names[h.Name] = true
	}
	if !names["block_io_latency_nvme0n1"] && !names["block_io_latency_sda"] {
		t.Errorf("expected nvme0n1 and sda, got: %v", names)
	}
}

// --- Event Parser Tests ---

func TestParseTabularEvents(t *testing.T) {
	raw := readTestdata(t, "tcpconnlat.txt")
	events, truncated := ParseTabularEvents(raw, 100)

	if truncated {
		t.Error("should not be truncated with high limit")
	}
	if len(events) == 0 {
		t.Fatal("no events parsed")
	}
	if len(events) != 6 {
		t.Errorf("expected 6 events, got %d", len(events))
	}

	// First event should have PID and COMM
	first := events[0]
	if first.PID != 3090 {
		t.Errorf("first event PID = %d, want 3090", first.PID)
	}
	if first.Comm != "curl" {
		t.Errorf("first event COMM = %q, want curl", first.Comm)
	}

	// Should have parsed DADDR as a detail
	if _, ok := first.Details["daddr"]; !ok {
		t.Error("missing DADDR in event details")
	}
}

func TestParseTabularEventsRateLimit(t *testing.T) {
	raw := readTestdata(t, "tcpconnlat.txt")
	events, truncated := ParseTabularEvents(raw, 3)

	if !truncated {
		t.Error("should be truncated with limit=3")
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events (limit), got %d", len(events))
	}
}

// --- Folded Stack Parser Tests ---

func TestParseFoldedStacks(t *testing.T) {
	raw := readTestdata(t, "profile_folded.txt")
	stacks, err := ParseFoldedStacks(raw, "on-cpu")
	if err != nil {
		t.Fatalf("ParseFoldedStacks: %v", err)
	}

	if len(stacks) == 0 {
		t.Fatal("no stacks parsed")
	}
	if len(stacks) != 8 {
		t.Errorf("expected 8 stacks, got %d", len(stacks))
	}

	// Should be sorted by count descending
	for i := 1; i < len(stacks); i++ {
		if stacks[i].Count > stacks[i-1].Count {
			t.Errorf("stacks not sorted: [%d].Count=%d > [%d].Count=%d",
				i, stacks[i].Count, i-1, stacks[i-1].Count)
		}
	}

	// Highest count should be the idle stack
	if stacks[0].Count != 52341 {
		t.Errorf("top stack count = %d, want 52341", stacks[0].Count)
	}
	if stacks[0].Type != "on-cpu" {
		t.Errorf("stack type = %q, want on-cpu", stacks[0].Type)
	}
}

// --- Tool-Specific Parser Tests ---

func TestParseBiolatency(t *testing.T) {
	raw := readTestdata(t, "biolatency.txt")
	result, err := ParseBiolatency(raw)
	if err != nil {
		t.Fatalf("ParseBiolatency: %v", err)
	}

	if result.Collector != "biolatency" {
		t.Errorf("collector = %q, want biolatency", result.Collector)
	}
	if result.Category != "disk" {
		t.Errorf("category = %q, want disk", result.Category)
	}
	if result.Tier != 2 {
		t.Errorf("tier = %d, want 2", result.Tier)
	}
	if len(result.Histograms) == 0 {
		t.Error("no histograms")
	}
}

func TestParseTcpconnlat(t *testing.T) {
	raw := readTestdata(t, "tcpconnlat.txt")
	result, err := ParseTcpconnlat(raw, 100)
	if err != nil {
		t.Fatalf("ParseTcpconnlat: %v", err)
	}

	if result.Collector != "tcpconnlat" {
		t.Errorf("collector = %q, want tcpconnlat", result.Collector)
	}
	if result.Category != "network" {
		t.Errorf("category = %q, want network", result.Category)
	}
	if len(result.Events) == 0 {
		t.Error("no events")
	}
}

func TestParseProfileStacks(t *testing.T) {
	raw := readTestdata(t, "profile_folded.txt")
	result, err := ParseProfileStacks(raw)
	if err != nil {
		t.Fatalf("ParseProfileStacks: %v", err)
	}

	if result.Collector != "profile" {
		t.Errorf("collector = %q, want profile", result.Collector)
	}
	if result.Category != "stacktrace" {
		t.Errorf("category = %q, want stacktrace", result.Category)
	}
	if len(result.Stacks) == 0 {
		t.Error("no stacks")
	}
}

// --- Ubuntu 24.04 Real-Output Tests ---

func TestParseBiolatencyUbuntu2404(t *testing.T) {
	raw := readTestdata(t, "biolatency_ubuntu2404.txt")
	result, err := ParseBiolatency(raw)
	if err != nil {
		t.Fatalf("ParseBiolatency: %v", err)
	}
	if result.Collector != "biolatency" {
		t.Errorf("collector = %q, want biolatency", result.Collector)
	}
	if len(result.Histograms) != 2 {
		t.Fatalf("expected 2 disk histograms, got %d", len(result.Histograms))
	}
	// Verify both disks are present
	names := map[string]bool{}
	for _, h := range result.Histograms {
		names[h.Name] = true
	}
	if !names["block_io_latency_sdb"] || !names["block_io_latency_sda"] {
		t.Errorf("expected sdb and sda histograms, got: %v", names)
	}
}

func TestParseRunqlatUbuntu2404(t *testing.T) {
	raw := readTestdata(t, "runqlat_ubuntu2404.txt")
	result, err := ParseRunqlat(raw)
	if err != nil {
		t.Fatalf("ParseRunqlat: %v", err)
	}
	if result.Collector != "runqlat" {
		t.Errorf("collector = %q, want runqlat", result.Collector)
	}
	if len(result.Histograms) == 0 {
		t.Fatal("no histograms parsed")
	}
	hist := result.Histograms[0]
	if hist.TotalCount == 0 {
		t.Error("totalCount = 0")
	}
	// Tail latency should reach into the tens-of-ms range (32768+ us bucket)
	if hist.Max < 32768 {
		t.Errorf("max = %v, expected >= 32768 us (tail to ~65ms)", hist.Max)
	}
}

func TestParseExt4slowerUbuntu2404(t *testing.T) {
	raw := readTestdata(t, "ext4slower_ubuntu2404.txt")
	result, err := ParseBiosnoop(raw, 100) // reuse tabular parser via ParseBiosnoop
	_ = result
	_ = err
	// Parse via ParseTabularEvents directly to verify preamble handling
	events, truncated := ParseTabularEvents(raw, 100)
	if truncated {
		t.Error("should not be truncated")
	}
	if len(events) == 0 {
		t.Fatal("no events parsed from ext4slower_ubuntu2404.txt")
	}
	// Check that events contain expected processes
	comms := map[string]bool{}
	for _, e := range events {
		comms[e.Comm] = true
	}
	if !comms["dockerd"] {
		t.Errorf("expected dockerd in events, got comms: %v", comms)
	}
	if !comms["kafka-raft-io"] {
		t.Errorf("expected kafka-raft-io in events, got comms: %v", comms)
	}
}

func TestParseCachestatUbuntu2404(t *testing.T) {
	raw := readTestdata(t, "cachestat_ubuntu2404.txt")
	result, err := ParseCachestat(raw)
	if err != nil {
		t.Fatalf("ParseCachestat: %v", err)
	}
	if result.Collector != "cachestat" {
		t.Errorf("collector = %q, want cachestat", result.Collector)
	}
	if len(result.Events) == 0 {
		t.Fatal("no events parsed from cachestat_ubuntu2404.txt")
	}
	// Each event should have MISSES and DIRTIES fields
	first := result.Events[0]
	if _, ok := first.Details["misses"]; !ok {
		t.Error("missing 'misses' field in cachestat event")
	}
	if _, ok := first.Details["dirties"]; !ok {
		t.Error("missing 'dirties' field in cachestat event")
	}
}

func TestParseHistogramWithPreamble(t *testing.T) {
	// Verify that ParseHistogram correctly skips preamble and parses buckets
	raw := readTestdata(t, "runqlat_ubuntu2404.txt")
	hist, err := ParseHistogram(raw, "run_queue_latency", "us")
	if err != nil {
		t.Fatalf("ParseHistogram with preamble: %v", err)
	}
	if len(hist.Buckets) == 0 {
		t.Fatal("no buckets parsed from file with Tracing preamble")
	}
}

func TestParseTabularEventsWithPreamble(t *testing.T) {
	// Verify that ParseTabularEvents skips "Tracing..." and "Attaching..." lines
	input := "Tracing ext4 operations slower than 1 ms\nTIME COMM PID LAT(ms)\n10:00:01 bash 1234 5.2\n"
	events, _ := ParseTabularEvents(input, 100)
	if len(events) == 0 {
		t.Fatal("no events parsed when preamble present")
	}
	if events[0].Comm != "bash" {
		t.Errorf("comm = %q, want bash", events[0].Comm)
	}

	// Also verify Attaching preamble is skipped
	input2 := "Attaching 10 probes...\nCOMM PID LAT\nngrok 555 3.1\n"
	events2, _ := ParseTabularEvents(input2, 100)
	if len(events2) == 0 {
		t.Fatal("no events parsed when Attaching preamble present")
	}
	if events2[0].Comm != "ngrok" {
		t.Errorf("comm = %q, want ngrok", events2[0].Comm)
	}
}

// --- Error Case Tests ---

func TestParseHistogramMalformed(t *testing.T) {
	malformed := "this is not a histogram\njust random text\nno buckets here"
	_, err := ParseHistogram(malformed, "test", "us")
	if err == nil {
		t.Error("expected error for malformed histogram")
	}
}

func TestParseFoldedStacksEmpty(t *testing.T) {
	stacks, _ := ParseFoldedStacks("", "test")
	if len(stacks) != 0 {
		t.Errorf("expected 0 stacks for empty input, got %d", len(stacks))
	}
}

func TestParseTabularEventsEmpty(t *testing.T) {
	events, truncated := ParseTabularEvents("", 100)
	if len(events) != 0 {
		t.Errorf("expected 0 events for empty input, got %d", len(events))
	}
	if truncated {
		t.Error("should not be truncated")
	}
}
