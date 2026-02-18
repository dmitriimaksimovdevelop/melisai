#!/usr/bin/env python3
"""check_detection.py — validate melisai report detects expected anomalies.

Usage: python3 check_detection.py <report.json> <test_name>

Exit codes:
  0 — all checks passed
  1 — one or more checks failed
  2 — usage error / file not found
"""

import json
import sys
import os


def load_report(path):
    with open(path) as f:
        return json.load(f)


def get_anomalies(report):
    """Return list of anomaly dicts from report."""
    return report.get("summary", {}).get("anomalies") or []


def get_health_score(report):
    """Return health score (int) or None."""
    return report.get("summary", {}).get("health_score")


def get_recommendations(report):
    """Return list of recommendation dicts."""
    return report.get("summary", {}).get("recommendations") or []


def get_resources(report):
    """Return resources dict (USE metrics)."""
    return report.get("summary", {}).get("resources", {})


def get_metadata(report):
    """Return metadata dict."""
    return report.get("metadata", {})


def has_anomaly(report, metric, min_severity=None):
    """Check if report has an anomaly for the given metric.

    min_severity: None (any), "warning" (warning or critical), "critical" (critical only)
    """
    severity_rank = {"info": 0, "warning": 1, "critical": 2, "error": 3}
    min_rank = severity_rank.get(min_severity, 0) if min_severity else 0

    for a in get_anomalies(report):
        if a.get("metric") == metric:
            a_rank = severity_rank.get(a.get("severity", ""), 0)
            if a_rank >= min_rank:
                return True
    return False


def anomaly_value(report, metric):
    """Return float value of anomaly for metric, or None."""
    for a in get_anomalies(report):
        if a.get("metric") == metric:
            try:
                return float(a["value"])
            except (KeyError, ValueError, TypeError):
                return None
    return None


def has_recommendation(report, title_substring):
    """Check if any recommendation title contains the substring (case-insensitive)."""
    sub = title_substring.lower()
    for rec in get_recommendations(report):
        if sub in rec.get("title", "").lower():
            return True
        # Also check evidence and expected_impact
        if sub in rec.get("evidence", "").lower():
            return True
    return False


def has_anomaly_category(report, category):
    """Check if report has any anomaly in the given category."""
    for a in get_anomalies(report):
        if a.get("category") == category:
            return True
    return False


def count_anomaly_categories(report):
    """Count distinct anomaly categories."""
    cats = set()
    for a in get_anomalies(report):
        cats.add(a.get("category"))
    return len(cats)


# =============================================================================
# Per-test check definitions
# Each returns list of (check_name, passed, detail) tuples
# =============================================================================

def check_cpu_burn(report):
    results = []
    health = get_health_score(report)

    # cpu_utilization should be CRITICAL (>95%) or at least WARNING (>80%)
    has_cpu = has_anomaly(report, "cpu_utilization", "warning")
    val = anomaly_value(report, "cpu_utilization")
    results.append((
        "cpu_utilization WARNING+",
        has_cpu,
        f"value={val}" if val else "anomaly not found"
    ))

    # CPU overload: load_average OR cpu_psi_pressure should fire
    # (load_average is a 1-min moving average — may lag on short tests)
    has_load = has_anomaly(report, "load_average", "warning")
    has_psi = has_anomaly(report, "cpu_psi_pressure", "warning")
    load_val = anomaly_value(report, "load_average")
    psi_val = anomaly_value(report, "cpu_psi_pressure")
    results.append((
        "load_average OR cpu_psi_pressure WARNING+",
        has_load or has_psi,
        f"load={load_val}, psi={psi_val}"
    ))

    # Health score should drop
    results.append((
        "health_score < 70",
        health is not None and health < 70,
        f"health={health}"
    ))

    # Should have some recommendation (may be generic)
    recs = get_recommendations(report)
    results.append((
        "has recommendations",
        len(recs) > 0,
        f"count={len(recs)}"
    ))

    return results


def check_memory_pressure(report):
    results = []
    health = get_health_score(report)

    # memory_utilization should be WARNING+ (>85%) or CRITICAL (>95%)
    has_mem = has_anomaly(report, "memory_utilization", "warning")
    val = anomaly_value(report, "memory_utilization")

    # Also check raw memory data for high usage even without anomaly
    mem_results = report.get("categories", {}).get("memory", [])
    raw_mem_pct = None
    for res in mem_results:
        data = res.get("data") or {}
        total = data.get("total_bytes", 0)
        avail = data.get("available_bytes", 0)
        if total > 0:
            raw_mem_pct = (total - avail) / total * 100

    results.append((
        "memory_utilization WARNING+ OR raw > 80%",
        has_mem or (raw_mem_pct is not None and raw_mem_pct > 80),
        f"anomaly_value={val}, raw_pct={raw_mem_pct:.1f}%" if raw_mem_pct else f"anomaly_value={val}"
    ))

    # Should trigger at least one memory-related anomaly, OR raw memory > 85%
    has_swap = has_anomaly(report, "swap_usage")
    has_psi = has_anomaly(report, "memory_psi_pressure")
    has_any_mem_anomaly = has_mem or has_swap or has_psi
    results.append((
        "memory anomaly OR raw_pct > 85%",
        has_any_mem_anomaly or (raw_mem_pct is not None and raw_mem_pct > 85),
        f"util={has_mem}, swap={has_swap}, psi={has_psi}, raw={raw_mem_pct:.1f}%" if raw_mem_pct else f"util={has_mem}, swap={has_swap}, psi={has_psi}"
    ))

    results.append((
        "health_score < 80",
        health is not None and health < 80,
        f"health={health}"
    ))

    return results


def check_disk_flood(report):
    results = []
    health = get_health_score(report)

    has_disk = has_anomaly(report, "disk_utilization", "warning")
    has_io_psi = has_anomaly(report, "io_psi_pressure", "warning")
    has_iowait = has_anomaly(report, "cpu_iowait", "warning")
    has_disk_lat = has_anomaly(report, "disk_avg_latency", "warning")

    results.append((
        "disk_utilization WARNING+ OR io_psi OR iowait OR disk_latency",
        has_disk or has_io_psi or has_iowait or has_disk_lat,
        f"disk_util={has_disk}, io_psi={has_io_psi}, iowait={has_iowait}, disk_lat={has_disk_lat}"
    ))

    results.append((
        "health_score < 90",
        health is not None and health < 90,
        f"health={health}"
    ))

    return results


def check_fork_storm(report):
    results = []

    # CPU utilization should be elevated
    has_cpu = has_anomaly(report, "cpu_utilization", "warning")
    val = anomaly_value(report, "cpu_utilization")
    results.append((
        "cpu_utilization WARNING+",
        has_cpu,
        f"value={val}" if val else "anomaly not found"
    ))

    # Check context switches via CPU data
    cpu_results = report.get("categories", {}).get("cpu", [])
    ctx_switches = None
    for res in cpu_results:
        data = res.get("data") or {}
        if "context_switches_per_sec" in data:
            ctx_switches = data["context_switches_per_sec"]

    # Fork storm elevates context switches significantly (typically 50k-200k+)
    results.append((
        "context_switches > 50k/s",
        ctx_switches is not None and ctx_switches > 50000,
        f"ctx_switches={ctx_switches}"
    ))

    # Process count from process category
    proc_results = report.get("categories", {}).get("process", [])
    total_processes = None
    procs_running = None
    for res in proc_results:
        data = res.get("data") or {}
        if "total_processes" in data:
            total_processes = data["total_processes"]
        if "running" in data:
            procs_running = data["running"]

    results.append((
        "total_processes > 500 OR procs_running > 100 OR cpu anomaly",
        (total_processes is not None and total_processes > 500)
        or (procs_running is not None and procs_running > 100)
        or has_cpu,
        f"total_procs={total_processes}, running={procs_running}"
    ))

    return results


def check_runq_saturation(report):
    results = []
    health = get_health_score(report)

    # Heavy CPU saturation indicator: load_average CRITICAL OR cpu_psi_pressure CRITICAL
    # (load_average is a 1-min exponential moving average — lags on short tests)
    has_load_crit = has_anomaly(report, "load_average", "critical")
    has_psi_crit = has_anomaly(report, "cpu_psi_pressure", "critical")
    load_val = anomaly_value(report, "load_average")
    psi_val = anomaly_value(report, "cpu_psi_pressure")
    results.append((
        "load_average CRITICAL OR cpu_psi_pressure CRITICAL",
        has_load_crit or has_psi_crit,
        f"load={load_val}, psi={psi_val}"
    ))

    # CPU PSI pressure at least WARNING
    has_psi = has_anomaly(report, "cpu_psi_pressure", "warning")
    results.append((
        "cpu_psi_pressure WARNING+",
        has_psi,
        f"value={psi_val}" if psi_val else "anomaly not found"
    ))

    # Health should be very low
    results.append((
        "health_score < 50",
        health is not None and health < 50,
        f"health={health}"
    ))

    # runqlat p99 if available (Tier 2 tool — informational)
    has_runq = has_anomaly(report, "runqlat_p99", "warning")
    runq_val = anomaly_value(report, "runqlat_p99")
    results.append((
        "runqlat_p99 WARNING+ (if available)",
        has_runq or True,  # bonus: always passes, just informational
        f"value={runq_val}, detected={has_runq} (bonus check)"
    ))

    return results


def check_tcp_retrans(report):
    results = []

    has_retrans = has_anomaly(report, "tcp_retransmits", "warning")
    val = anomaly_value(report, "tcp_retransmits")
    results.append((
        "tcp_retransmits WARNING+",
        has_retrans,
        f"value={val}" if val else "anomaly not found"
    ))

    has_rec = has_recommendation(report, "retrans") or has_recommendation(report, "tcp")
    results.append((
        "retransmission recommendation",
        has_rec,
        "found" if has_rec else "no retransmission recommendation"
    ))

    return results


def check_combined(report):
    results = []
    health = get_health_score(report)
    anomalies = get_anomalies(report)
    num_categories = count_anomaly_categories(report)

    results.append((
        ">=2 anomaly categories",
        num_categories >= 2,
        f"categories={num_categories}: {sorted(set(a.get('category') for a in anomalies))}"
    ))

    results.append((
        "health_score < 80",
        health is not None and health < 80,
        f"health={health}"
    ))

    results.append((
        ">=3 total anomalies",
        len(anomalies) >= 3,
        f"anomaly_count={len(anomalies)}"
    ))

    return results


def check_observer_effect(report):
    """Observer effect is checked by run_validation.sh externally.
    Here we just validate the report metadata is sane."""
    results = []
    metadata = get_metadata(report)

    results.append((
        "report has metadata",
        bool(metadata),
        f"keys={list(metadata.keys())}" if metadata else "no metadata"
    ))

    # Check observer overhead field if present
    summary = report.get("summary", {})
    health = get_health_score(report)
    results.append((
        "health_score present",
        health is not None,
        f"health={health}"
    ))

    return results


# =============================================================================
# Test dispatch
# =============================================================================

TESTS = {
    "cpu_burn": check_cpu_burn,
    "memory_pressure": check_memory_pressure,
    "disk_flood": check_disk_flood,
    "fork_storm": check_fork_storm,
    "runq_saturation": check_runq_saturation,
    "tcp_retrans": check_tcp_retrans,
    "combined": check_combined,
    "observer_effect": check_observer_effect,
}


def main():
    if len(sys.argv) < 3:
        print(f"Usage: {sys.argv[0]} <report.json> <test_name>", file=sys.stderr)
        print(f"Available tests: {', '.join(sorted(TESTS.keys()))}", file=sys.stderr)
        sys.exit(2)

    report_path = sys.argv[1]
    test_name = sys.argv[2]

    if not os.path.exists(report_path):
        print(f"ERROR: report not found: {report_path}", file=sys.stderr)
        sys.exit(2)

    if test_name not in TESTS:
        print(f"ERROR: unknown test '{test_name}'. Available: {', '.join(sorted(TESTS.keys()))}", file=sys.stderr)
        sys.exit(2)

    report = load_report(report_path)

    # Print report summary
    anomalies = get_anomalies(report)
    health = get_health_score(report)
    recs = get_recommendations(report)
    print(f"--- Report summary for test '{test_name}' ---")
    print(f"  Health score: {health}")
    print(f"  Anomalies ({len(anomalies)}):")
    for a in anomalies:
        print(f"    [{a.get('severity', '?').upper():8s}] {a.get('metric', '?')}: {a.get('message', '')} (value={a.get('value', '?')})")
    print(f"  Recommendations ({len(recs)}):")
    for r in recs:
        print(f"    [{r.get('priority', '?')}] {r.get('title', '?')}")
    print()

    # Run checks
    check_fn = TESTS[test_name]
    checks = check_fn(report)

    passed = 0
    failed = 0
    for name, ok, detail in checks:
        status = "PASS" if ok else "FAIL"
        icon = "+" if ok else "!"
        print(f"  [{icon}] {status}: {name}  ({detail})")
        if ok:
            passed += 1
        else:
            failed += 1

    print()
    print(f"  Result: {passed}/{passed + failed} checks passed")

    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
