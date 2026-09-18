---
kind: skill
name: csv-analyzer
description: Analyze a CSV file and flag rows that stand out.
version: 0.1.0
entrypoint: scripts/main.py
test: python3 -m unittest discover -s tests -p "test_*.py"
---

# CSV Analyzer

Given a CSV file, flags rows whose numeric columns are unusual outliers
(more than N standard deviations from that column's mean).

## Usage

```
python3 scripts/main.py path/to/file.csv [--threshold 2.0]
```

See `samples/sample.csv` for an example input -- one order in it (row 5,
`amount: 412.00`) is a deliberate outlier so you can see the skill flag it:

```
$ python3 scripts/main.py samples/sample.csv
row 5, column 'amount': 412 is 2.6 std dev from the mean (86.82)
```

## Implementation

`scripts/main.py` does the analysis; `tests/test_main.py` covers it with
stdlib `unittest` (no extra install needed) and the `test:` command above
runs it via `agentworks test`.
