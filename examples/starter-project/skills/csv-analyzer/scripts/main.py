#!/usr/bin/env python3
"""csv-analyzer: flag rows in a CSV whose numeric columns are unusually far
from that column's mean (a simple standard-deviation outlier check).

Usage:
    python3 scripts/main.py path/to/file.csv [--threshold 2.0]
"""

import argparse
import csv
import statistics
import sys


def numeric_columns(rows: list[dict[str, str]]) -> dict[str, list[float]]:
    columns: dict[str, list[float]] = {}
    if not rows:
        return columns
    for field in rows[0]:
        values = []
        for row in rows:
            try:
                values.append(float(row[field]))
            except (TypeError, ValueError):
                values = []
                break
        if values:
            columns[field] = values
    return columns


def find_outliers(rows: list[dict[str, str]], threshold: float) -> list[tuple[int, str, str]]:
    columns = numeric_columns(rows)
    outliers = []
    for field, values in columns.items():
        if len(values) < 2:
            continue
        mean = statistics.mean(values)
        stdev = statistics.pstdev(values)
        if stdev == 0:
            continue
        for i, value in enumerate(values):
            z = abs(value - mean) / stdev
            if z >= threshold:
                outliers.append((i, field, f"{value:g} is {z:.1f} std dev from the mean ({mean:.2f})"))
    return outliers


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("csv_path")
    parser.add_argument("--threshold", type=float, default=2.0)
    args = parser.parse_args()

    with open(args.csv_path, newline="", encoding="utf-8") as f:
        rows = list(csv.DictReader(f))

    outliers = find_outliers(rows, args.threshold)
    if not outliers:
        print("No rows stand out.")
        return 0

    for row_index, field, reason in sorted(outliers):
        print(f"row {row_index + 1}, column {field!r}: {reason}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
