"""Tests for csv-analyzer. Run with: python3 -m unittest discover -s tests -t .

Uses stdlib unittest (no extra install needed) so `agentworks test` works
out of the box; swap in pytest if your project already depends on it.
"""

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent / "scripts"))

from main import find_outliers  # noqa: E402


class TestFindOutliers(unittest.TestCase):
    def test_flags_the_obvious_outlier(self):
        # Same amounts as samples/sample.csv: one order (index 4) is a
        # clear outlier among the rest.
        rows = [
            {"amount": "42.50"},
            {"amount": "38.10"},
            {"amount": "41.00"},
            {"amount": "39.75"},
            {"amount": "412.00"},  # clearly stands out
            {"amount": "40.20"},
            {"amount": "37.90"},
            {"amount": "43.10"},
        ]
        outliers = find_outliers(rows, threshold=2.0)
        flagged_rows = {i for i, _field, _reason in outliers}
        self.assertIn(4, flagged_rows)

    def test_no_outliers_when_values_are_uniform(self):
        rows = [{"amount": "10"} for _ in range(5)]
        self.assertEqual(find_outliers(rows, threshold=2.0), [])


if __name__ == "__main__":
    unittest.main()
