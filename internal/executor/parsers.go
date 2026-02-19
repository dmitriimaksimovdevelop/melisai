package executor

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// ansiEscapeRe matches ANSI terminal escape sequences (e.g. color codes).
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[mGKHF]`)

// stripANSI removes ANSI terminal escape sequences from s.
func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

// isPreambleLine returns true for BCC tool status lines that appear before the
// actual data (e.g. "Tracing block device I/O... Hit Ctrl-C to end.").
func isPreambleLine(line string) bool {
	return strings.HasPrefix(line, "Tracing") || strings.HasPrefix(line, "Attaching")
}

// --- Histogram Parser ---

// ParseHistogram parses BCC-style power-of-2 histogram output.
// Format:
//
//	usecs     : count   distribution
//	  0 -> 1  : 10     |**                            |
//	  2 -> 3  : 50     |***********                   |
//
// Returns a Histogram with ordered buckets and computed percentiles.
// ANSI escape codes in raw are stripped before parsing.
func ParseHistogram(raw string, name, unit string) (*model.Histogram, error) {
	raw = stripANSI(raw)
	lines := strings.Split(raw, "\n")
	var buckets []model.HistBucket

	bucketRe := regexp.MustCompile(`^\s*(\d+)\s*->\s*(\d+)\s*:\s*(\d+)`)

	for _, line := range lines {
		matches := bucketRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		low, _ := strconv.ParseInt(matches[1], 10, 64)
		high, _ := strconv.ParseInt(matches[2], 10, 64)
		count, _ := strconv.ParseInt(matches[3], 10, 64)
		buckets = append(buckets, model.HistBucket{Low: low, High: high, Count: count})
	}

	if len(buckets) == 0 {
		return nil, ErrNoHistogramData
	}

	// Compute statistics
	hist := &model.Histogram{
		Name:    name,
		Unit:    unit,
		Buckets: buckets,
	}
	computeHistStats(hist)
	return hist, nil
}

// ErrNoHistogramData is returned when no histogram buckets are found in output.
// This is normal for tools that had no events during the collection period.
var ErrNoHistogramData = fmt.Errorf("no histogram buckets found")

// ParseHistogramResult wraps ParseHistogram and returns an empty Result
// instead of an error when no histogram data is found.
func ParseHistogramResult(raw, collector, category, histName, unit string) (*model.Result, error) {
	hist, err := ParseHistogram(raw, histName, unit)
	if err != nil {
		if errors.Is(err, ErrNoHistogramData) {
			return &model.Result{Collector: collector, Category: category, Tier: 2}, nil
		}
		return nil, err
	}
	return &model.Result{Collector: collector, Category: category, Tier: 2, Histograms: []model.Histogram{*hist}}, nil
}

// ParsePerDiskHistogram parses biolatency -D style output with per-disk sections.
func ParsePerDiskHistogram(raw string, unit string) ([]model.Histogram, error) {
	// Split into per-disk sections
	// Format: "disk = 'nvme0n1'" followed by histogram lines
	sections := splitDiskSections(raw)
	var histograms []model.Histogram

	for diskName, section := range sections {
		h, err := ParseHistogram(section, "block_io_latency_"+diskName, unit)
		if err != nil {
			continue
		}
		histograms = append(histograms, *h)
	}

	if len(histograms) == 0 {
		// Try parsing as single histogram
		h, err := ParseHistogram(raw, "block_io_latency", unit)
		if err != nil {
			return nil, err
		}
		histograms = append(histograms, *h)
	}
	return histograms, nil
}

func splitDiskSections(raw string) map[string]string {
	sections := make(map[string]string)
	diskRe := regexp.MustCompile(`(?i)disk\s*=\s*'?(\w+)'?`)

	lines := strings.Split(raw, "\n")
	currentDisk := ""
	var currentLines []string

	for _, line := range lines {
		matches := diskRe.FindStringSubmatch(line)
		if matches != nil {
			if currentDisk != "" && len(currentLines) > 0 {
				sections[currentDisk] = strings.Join(currentLines, "\n")
			}
			currentDisk = matches[1]
			currentLines = nil
		} else if currentDisk != "" {
			currentLines = append(currentLines, line)
		}
	}
	if currentDisk != "" && len(currentLines) > 0 {
		sections[currentDisk] = strings.Join(currentLines, "\n")
	}
	return sections
}

func computeHistStats(h *model.Histogram) {
	var totalCount int64
	var weightedSum float64

	for _, b := range h.Buckets {
		totalCount += b.Count
		mid := float64(b.Low+b.High) / 2.0
		weightedSum += mid * float64(b.Count)
	}

	h.TotalCount = totalCount
	if totalCount > 0 {
		h.Mean = weightedSum / float64(totalCount)
	}

	// Compute percentiles
	h.P50 = computePercentile(h.Buckets, totalCount, 0.50)
	h.P90 = computePercentile(h.Buckets, totalCount, 0.90)
	h.P99 = computePercentile(h.Buckets, totalCount, 0.99)
	h.P999 = computePercentile(h.Buckets, totalCount, 0.999)

	if len(h.Buckets) > 0 {
		h.Max = float64(h.Buckets[len(h.Buckets)-1].High)
	}
}

func computePercentile(buckets []model.HistBucket, totalCount int64, pct float64) float64 {
	target := int64(math.Ceil(float64(totalCount) * pct))
	var cumulative int64

	for _, b := range buckets {
		cumulative += b.Count
		if cumulative >= target {
			// Linear interpolation within bucket
			mid := float64(b.Low+b.High) / 2.0
			return mid
		}
	}
	if len(buckets) > 0 {
		return float64(buckets[len(buckets)-1].High)
	}
	return 0
}

// --- Event Parser ---

// ParseTabularEvents parses BCC tabular output (e.g., tcpconnlat, tcpretrans).
// It skips preamble lines that start with "Tracing" or "Attaching" before the
// header, and tolerates data lines with more or fewer fields than the header.
func ParseTabularEvents(raw string, maxEvents int) ([]model.Event, bool) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return nil, false
	}

	// Skip any preamble lines (e.g. "Tracing ... Hit Ctrl-C to end.") to find
	// the actual header line.
	headerIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isPreambleLine(trimmed) {
			continue
		}
		headerIdx = i
		break
	}
	if headerIdx < 0 {
		return nil, false
	}

	headers := strings.Fields(lines[headerIdx])
	if len(headers) == 0 {
		return nil, false
	}

	var events []model.Event
	truncated := false

	for _, line := range lines[headerIdx+1:] {
		line = strings.TrimSpace(line)
		if line == "" || isPreambleLine(line) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		event := model.Event{
			Details: make(map[string]interface{}),
		}

		// Iterate over the shorter of headers vs fields to handle mismatches.
		limit := len(headers)
		if len(fields) < limit {
			limit = len(fields)
		}

		for i := 0; i < limit; i++ {
			headerLower := strings.ToLower(headers[i])
			switch headerLower {
			case "time", "time(s)":
				event.Time = fields[i]
			case "pid":
				event.PID, _ = strconv.Atoi(fields[i])
			case "comm":
				event.Comm = fields[i]
			default:
				// Try to parse as number, otherwise keep as string
				if v, err := strconv.ParseFloat(fields[i], 64); err == nil {
					event.Details[headerLower] = v
				} else {
					event.Details[headerLower] = fields[i]
				}
			}
		}

		events = append(events, event)
		if maxEvents > 0 && len(events) >= maxEvents {
			truncated = true
			break
		}
	}

	return events, truncated
}

// --- Folded Stack Parser ---

// ParseFoldedStacks parses folded stack trace output (e.g., from profile -f).
// Format: "func1;func2;func3 count"
func ParseFoldedStacks(raw string, stackType string) ([]model.StackTrace, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	var stacks []model.StackTrace

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Find last space -- everything before is the stack, after is the count
		lastSpace := strings.LastIndex(line, " ")
		if lastSpace < 0 {
			continue
		}

		stack := line[:lastSpace]
		countStr := line[lastSpace+1:]
		count, err := strconv.Atoi(countStr)
		if err != nil {
			continue
		}

		stacks = append(stacks, model.StackTrace{
			Stack: stack,
			Count: count,
			Type:  stackType,
		})
	}

	// Sort by count descending
	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].Count > stacks[j].Count
	})

	return stacks, nil
}

// --- Specific Tool Parsers ---

// ParseRunqlat parses runqlat histogram output.
func ParseRunqlat(raw string) (*model.Result, error) {
	return ParseHistogramResult(raw, "runqlat", "cpu", "run_queue_latency", "us")
}

// ParseBiolatency parses biolatency output (possibly per-disk with -D flag).
func ParseBiolatency(raw string) (*model.Result, error) {
	hists, err := ParsePerDiskHistogram(raw, "us")
	if err != nil {
		// No histogram data (e.g. no disk I/O during collection) -- return empty result
		return &model.Result{
			Collector: "biolatency",
			Category:  "disk",
			Tier:      2,
		}, nil
	}
	return &model.Result{
		Collector:  "biolatency",
		Category:   "disk",
		Tier:       2,
		Histograms: hists,
	}, nil
}

// ParseTcpconnlat parses tcpconnlat tabular event output.
func ParseTcpconnlat(raw string, maxEvents int) (*model.Result, error) {
	events, truncated := ParseTabularEvents(raw, maxEvents)
	return &model.Result{
		Collector: "tcpconnlat",
		Category:  "network",
		Tier:      2,
		Events:    events,
		Truncated: truncated,
	}, nil
}

// ParseTcpretrans parses tcpretrans tabular event output.
func ParseTcpretrans(raw string, maxEvents int) (*model.Result, error) {
	events, truncated := ParseTabularEvents(raw, maxEvents)
	return &model.Result{
		Collector: "tcpretrans",
		Category:  "network",
		Tier:      2,
		Events:    events,
		Truncated: truncated,
	}, nil
}

// ParseTcprtt parses tcprtt histogram output.
func ParseTcprtt(raw string) (*model.Result, error) {
	return ParseHistogramResult(raw, "tcprtt", "network", "tcp_rtt", "us")
}

// ParseGethostlatency parses gethostlatency tabular output.
func ParseGethostlatency(raw string, maxEvents int) (*model.Result, error) {
	events, truncated := ParseTabularEvents(raw, maxEvents)
	return &model.Result{
		Collector: "gethostlatency",
		Category:  "network",
		Tier:      2,
		Events:    events,
		Truncated: truncated,
	}, nil
}

// ParseBiosnoop parses biosnoop tabular event output.
func ParseBiosnoop(raw string, maxEvents int) (*model.Result, error) {
	events, truncated := ParseTabularEvents(raw, maxEvents)
	return &model.Result{
		Collector: "biosnoop",
		Category:  "disk",
		Tier:      2,
		Events:    events,
		Truncated: truncated,
	}, nil
}

// ParseTcpdrop parses tcpdrop output (events + kernel stacks).
func ParseTcpdrop(raw string, maxEvents int) (*model.Result, error) {
	events, truncated := ParseTabularEvents(raw, maxEvents)
	// tcpdrop also contains kernel stacks -- parse those too
	stacks, _ := extractInlineStacks(raw)
	return &model.Result{
		Collector: "tcpdrop",
		Category:  "network",
		Tier:      2,
		Events:    events,
		Stacks:    stacks,
		Truncated: truncated,
	}, nil
}

// extractInlineStacks pulls kernel stack traces from inline output like tcpdrop.
func extractInlineStacks(raw string) ([]model.StackTrace, error) {
	lines := strings.Split(raw, "\n")
	var stacks []model.StackTrace
	var currentStack []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") || // kernel address
			strings.Contains(trimmed, "+0x") { // kernel symbol+offset
			currentStack = append(currentStack, trimmed)
		} else if len(currentStack) > 0 {
			stacks = append(stacks, model.StackTrace{
				Stack: strings.Join(currentStack, ";"),
				Count: 1,
				Type:  "kernel",
			})
			currentStack = nil
		}
	}
	if len(currentStack) > 0 {
		stacks = append(stacks, model.StackTrace{
			Stack: strings.Join(currentStack, ";"),
			Count: 1,
			Type:  "kernel",
		})
	}
	return stacks, nil
}

// ParseProfileStacks parses `profile -af` folded stack output.
func ParseProfileStacks(raw string) (*model.Result, error) {
	stacks, err := ParseFoldedStacks(raw, "on-cpu")
	if err != nil {
		return nil, err
	}

	totalSamples := 0
	for _, s := range stacks {
		totalSamples += s.Count
	}

	return &model.Result{
		Collector: "profile",
		Category:  "stacktrace",
		Tier:      2,
		Stacks:    stacks,
		Data: &model.StackData{
			TotalSamples: totalSamples,
			UniqueStacks: len(stacks),
		},
	}, nil
}

// ParseOffcputime parses offcputime -fK folded stack output.
func ParseOffcputime(raw string) (*model.Result, error) {
	stacks, err := ParseFoldedStacks(raw, "off-cpu")
	if err != nil {
		return nil, err
	}

	var totalUs int64
	for _, s := range stacks {
		totalUs += int64(s.Count)
	}

	return &model.Result{
		Collector: "offcputime",
		Category:  "stacktrace",
		Tier:      2,
		Stacks:    stacks,
		Data: &model.StackData{
			TotalSamples: len(stacks),
			UniqueStacks: len(stacks),
			TotalUs:      totalUs,
		},
	}, nil
}

// ParseCachestat parses cachestat periodic output.
func ParseCachestat(raw string) (*model.Result, error) {
	events, _ := ParseTabularEvents(raw, 100)
	return &model.Result{
		Collector: "cachestat",
		Category:  "memory",
		Tier:      2,
		Events:    events,
	}, nil
}

// ParseExecsnoop parses execsnoop tabular output.
func ParseExecsnoop(raw string, maxEvents int) (*model.Result, error) {
	events, truncated := ParseTabularEvents(raw, maxEvents)
	return &model.Result{
		Collector: "execsnoop",
		Category:  "process",
		Tier:      2,
		Events:    events,
		Truncated: truncated,
	}, nil
}
