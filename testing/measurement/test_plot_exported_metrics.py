#!/usr/bin/env python3

import sys
import tempfile
import unittest
from pathlib import Path

import pandas as pd

sys.path.insert(0, str(Path(__file__).parent))

from plot_exported_metrics import build_marker_intervals


class MarkerIntervalTests(unittest.TestCase):
    def test_teardown_phase_wins_over_stale_measurement_window(self):
        start = pd.Timestamp('2026-05-23T18:59:18.661Z')

        with tempfile.TemporaryDirectory() as tmp:
            input_dir = Path(tmp)

            def write_marker(title, values):
                rows = ['timestamp,datetime,elapsed_seconds,value\n']
                for seconds, value in values:
                    dt = start + pd.Timedelta(seconds=seconds)
                    rows.append(
                        f"{dt.timestamp():.3f},{dt.isoformat().replace('+00:00', 'Z')},{seconds},{value}\n"
                    )
                (input_dir / f'{title}.csv').write_text(''.join(rows))

            write_marker('Load Test Phase', [(0, 2), (60, 2), (75, 3), (90, 3)])
            write_marker('Load Test Level VUs', [(0, 75), (60, 75), (75, 75), (90, 0)])
            write_marker('Load Test Measurement Window', [(0, 1), (60, 1), (75, 1), (90, 0)])

            intervals = build_marker_intervals(input_dir, start, max_elapsed=90)

        self.assertEqual(
            intervals,
            [
                {'start': 0.0, 'end': 60.0, 'phase': 'measure', 'level_vus': 75, 'source': 'marker'},
                {'start': 60.0, 'end': 90.0, 'phase': 'teardown', 'level_vus': 0, 'source': 'marker'},
            ],
        )


if __name__ == '__main__':
    unittest.main()
