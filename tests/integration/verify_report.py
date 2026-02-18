#!/usr/bin/env python3
"""Validate melisai JSON report structure.

Usage: python3 verify_report.py /path/to/report.json
Exit 0 = valid, exit 1 = invalid (prints errors to stderr).
"""

import json
import sys


def fail(msg):
    print(f"FAIL: {msg}", file=sys.stderr)
    return False


def validate(report):
    ok = True

    # Top-level keys
    for key in ("metadata", "categories", "summary"):
        if key not in report:
            ok = fail(f"missing top-level key: {key}") and False

    if not ok:
        return False

    # --- Metadata ---
    meta = report["metadata"]
    if meta.get("tool") != "melisai":
        ok = fail(f"metadata.tool = {meta.get('tool')!r}, expected 'melisai'") and False

    overhead = meta.get("observer_overhead")
    if overhead is not None:
        if not isinstance(overhead.get("self_pid"), int) or overhead["self_pid"] <= 0:
            ok = fail(f"observer_overhead.self_pid invalid: {overhead.get('self_pid')}") and False

    # --- Categories ---
    cats = report["categories"]
    if not isinstance(cats, dict):
        return fail("categories is not an object")

    required_cats = {"cpu", "memory", "process"}
    missing = required_cats - set(cats.keys())
    if missing:
        ok = fail(f"missing categories: {missing}") and False

    # Validate each result
    total_results = 0
    error_only_collectors = []
    for cat_name, results in cats.items():
        if not isinstance(results, list):
            ok = fail(f"categories.{cat_name} is not an array") and False
            continue
        for i, r in enumerate(results):
            total_results += 1
            collector = r.get("collector", f"unknown[{i}]")

            if "collector" not in r:
                ok = fail(f"categories.{cat_name}[{i}]: missing 'collector'") and False
            if "category" not in r:
                ok = fail(f"{collector}: missing 'category'") and False
            if "tier" not in r:
                ok = fail(f"{collector}: missing 'tier'") and False
            elif r["tier"] not in (1, 2, 3):
                ok = fail(f"{collector}: tier={r['tier']}, expected 1/2/3") and False

            # Check for error-only collectors (has errors but no data/histograms/events)
            has_errors = bool(r.get("errors"))
            has_data = r.get("data") is not None and r.get("data") != {}
            has_histograms = bool(r.get("histograms"))
            has_events = bool(r.get("events"))
            has_stacks = bool(r.get("stacks"))
            if has_errors and not (has_data or has_histograms or has_events or has_stacks):
                error_only_collectors.append(collector)

    if total_results == 0:
        ok = fail("no results in any category") and False

    # Warn about error-only collectors but don't fail (some tools legitimately have no data)
    if error_only_collectors:
        print(f"WARNING: {len(error_only_collectors)} error-only collectors: "
              f"{', '.join(error_only_collectors[:5])}"
              f"{'...' if len(error_only_collectors) > 5 else ''}", file=sys.stderr)

    # --- Summary ---
    summary = report["summary"]
    hs = summary.get("health_score")
    if not isinstance(hs, int) or hs < 0 or hs > 100:
        ok = fail(f"summary.health_score invalid: {hs!r}") and False

    return ok


def main():
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <report.json>", file=sys.stderr)
        sys.exit(2)

    path = sys.argv[1]
    try:
        with open(path) as f:
            report = json.load(f)
    except json.JSONDecodeError as e:
        print(f"FAIL: invalid JSON: {e}", file=sys.stderr)
        sys.exit(1)
    except FileNotFoundError:
        print(f"FAIL: file not found: {path}", file=sys.stderr)
        sys.exit(1)

    if validate(report):
        print(f"OK: report valid ({len(report.get('categories', {}))} categories)")
        sys.exit(0)
    else:
        sys.exit(1)


if __name__ == "__main__":
    main()
