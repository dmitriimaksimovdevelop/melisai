package executor

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// AggregateEvents groups and summarizes events for top-N analysis.
type AggregatedResult struct {
	TopByCount []AggregatedEntry `json:"top_by_count"`
	TotalCount int               `json:"total_count"`
}

type AggregatedEntry struct {
	Key    string                 `json:"key"`
	Count  int                    `json:"count"`
	Sample map[string]interface{} `json:"sample"` // one representative event
}

// AggregateByField groups events by a specific field and returns top-N.
func AggregateByField(events []model.Event, field string, topN int) *AggregatedResult {
	counts := make(map[string]int)
	samples := make(map[string]map[string]interface{})

	for _, event := range events {
		var key string
		if val, ok := event.Details[field]; ok {
			key = formatKey(val)
		} else {
			continue
		}
		counts[key]++
		if _, exists := samples[key]; !exists {
			samples[key] = event.Details
		}
	}

	entries := make([]AggregatedEntry, 0, len(counts))
	for key, count := range counts {
		entries = append(entries, AggregatedEntry{
			Key:    key,
			Count:  count,
			Sample: samples[key],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	if topN > 0 && len(entries) > topN {
		entries = entries[:topN]
	}

	total := 0
	for _, c := range counts {
		total += c
	}

	return &AggregatedResult{
		TopByCount: entries,
		TotalCount: total,
	}
}

// AggregateRetransmits groups tcpretrans events by destination IP.
func AggregateRetransmits(events []model.Event) *AggregatedResult {
	return AggregateByField(events, "raddr", 10)
}

// AggregateConnections groups tcpconnlat events by destination IP and computes avg latency.
func AggregateConnections(events []model.Event) *AggregatedResult {
	type connStats struct {
		count    int
		totalLat float64
		sample   map[string]interface{}
	}

	stats := make(map[string]*connStats)
	for _, event := range events {
		key := ""
		if raddr, ok := event.Details["daddr"]; ok {
			key = formatKey(raddr)
		} else if raddr, ok := event.Details["raddr"]; ok {
			key = formatKey(raddr)
		}
		if key == "" {
			continue
		}

		if _, exists := stats[key]; !exists {
			stats[key] = &connStats{sample: event.Details}
		}
		stats[key].count++
		if lat, ok := event.Details["lat(ms)"]; ok {
			if latFloat, err := parseFloat(lat); err == nil {
				stats[key].totalLat += latFloat
			}
		}
	}

	entries := make([]AggregatedEntry, 0, len(stats))
	for key, s := range stats {
		entry := AggregatedEntry{
			Key:    key,
			Count:  s.count,
			Sample: s.sample,
		}
		if s.count > 0 {
			entry.Sample["avg_lat_ms"] = s.totalLat / float64(s.count)
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})

	if len(entries) > 10 {
		entries = entries[:10]
	}

	total := 0
	for _, s := range stats {
		total += s.count
	}

	return &AggregatedResult{
		TopByCount: entries,
		TotalCount: total,
	}
}

// formatKey converts an interface{} to string without reflection-heavy fmt.Sprintf.
func formatKey(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(val, 64)
	case int:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
