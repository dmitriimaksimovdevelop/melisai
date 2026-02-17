# Chapter 13: AI Integration

## Overview

sysdiag generates a structured prompt that can be fed to any LLM (GPT-4, Claude, Gemini, etc.) for automated analysis. The prompt includes the report data plus a curated database of 21 anti-patterns.

## Source File: ai_prompt.go

- **Lines**: ~103
- **Purpose**: Generate context-aware AI analysis prompts

## Prompt Structure

```go
func GenerateAIPrompt(report *Report) string {
    prompt := "SYSTEM PERFORMANCE ANALYSIS\n\n"
    prompt += "You are an expert Linux performance engineer.\n"
    prompt += "Analyze the following system diagnostic report and provide:\n"
    prompt += "1. Root cause analysis of any anomalies\n"
    prompt += "2. Severity assessment\n"
    prompt += "3. Specific remediation steps\n"
    prompt += "4. Check for known anti-patterns\n\n"

    // Include relevant anti-patterns based on observed data
    prompt += generateAntiPatterns(report)

    // Include the full report data
    prompt += "\n--- REPORT DATA ---\n"
    prompt += jsonMarshal(report)
}
```

## Anti-Pattern Database

The AI prompt includes known performance anti-patterns, filtered based on what's relevant to the current report:

| ID | Anti-Pattern | Trigger Condition |
|----|-------------|-------------------|
| P1 | Single-CPU saturation | Per-CPU data shows 1 core at 99%+ |
| P2 | IOWait misconception | IOWait > 20% but disk util < 50% |
| P3 | Memory "leak" (actually cache) | MemFree low but Available high |
| P4 | Swap with available memory | Swap used but MemAvailable > 20% |
| P5 | Container throttling | nr_throttled > 0 |
| P6 | CLOSE_WAIT leak | close_wait_count > 0 |
| P7 | TIME_WAIT exhaustion | time_wait > 30000 |
| P8 | Zombie accumulation | zombie_count > 10 |
| P9 | FD exhaustion | Any process FDs > 5000 |
| P10 | NUMA imbalance | numa_miss / (hit+miss) > 10% |
| P11 | Memory fragmentation | All high-order buddyinfo = 0 |
| P12 | Disk scheduler mismatch | SSD with mq-deadline / HDD with none |
| P13 | Cubic on WAN | tcp_congestion = cubic + RetransSegs > 1% |
| P14 | Small TCP buffers | tcp_rmem max < 4MB |
| P15 | Low somaxconn | somaxconn < 128 with high PassiveOpens |
| P16 | High context switches | ctx_switches > 100K/s |
| P17 | Steal time on VM | steal > 5% |
| P18 | SoftIRQ storm | softirq > 20% on any CPU |
| P19 | Dirty page buildup | DirtyBytes > 1GB |
| P20 | OOM in dmesg | dmesg contains "Out of memory" |
| P21 | ECC errors | dmesg contains "EDAC" |

## Context-Aware Filtering

Only relevant anti-patterns are included in the prompt:

```go
func generateAntiPatterns(report *Report) string {
    var patterns []string
    if hasCPUData(report) {
        patterns = append(patterns, cpuPatterns...)
    }
    if hasContainerData(report) {
        patterns = append(patterns, containerPatterns...)
    }
    // Only include patterns for which data exists
}
```

This keeps the prompt focused and avoids wasting AI tokens on irrelevant patterns.

## Usage

```bash
sudo sysdiag collect --ai-prompt --output report.json
```

This adds an `ai_prompt` field to the JSON output that can be directly sent to an AI API.

---

*Next: [Chapter 14 — Report Diffing](14-report-diffing.md)*
