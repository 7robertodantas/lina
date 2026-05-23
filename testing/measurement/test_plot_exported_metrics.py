#!/usr/bin/env python3

import sys
import tempfile
import unittest
from pathlib import Path

import pandas as pd

sys.path.insert(0, str(Path(__file__).parent))

from plot_exported_metrics import (
    build_marker_intervals,
    collect_metric_csv_groups,
    conversion_for_metric,
    expected_rate_points,
    legend_label_for_column,
)


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

    def test_expected_rate_ramps_during_warmup_and_holds_during_measurement(self):
        intervals = [
            {'start': 0.0, 'end': 60.0, 'phase': 'warmup', 'level_vus': 25},
            {'start': 60.0, 'end': 180.0, 'phase': 'measure', 'level_vus': 25},
            {'start': 180.0, 'end': 240.0, 'phase': 'warmup', 'level_vus': 50},
            {'start': 240.0, 'end': 360.0, 'phase': 'measure', 'level_vus': 50},
        ]

        points_x, points_y = expected_rate_points(intervals, report_interval_seconds=1)

        self.assertEqual(points_x, [0.0, 60.0, 180.0, 240.0, 360.0])
        self.assertEqual(points_y, [0.0, 25.0, 25.0, 50.0, 50.0])

    def test_upward_level_change_during_stale_measure_phase_is_warmup(self):
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

            write_marker('Load Test Phase', [(0, 2), (60, 2), (75, 2), (90, 1), (105, 2)])
            write_marker('Load Test Level VUs', [(0, 25), (60, 25), (75, 50), (90, 50), (105, 50)])
            write_marker('Load Test Measurement Window', [(0, 1), (60, 1), (75, 1), (90, 0), (105, 1)])

            intervals = build_marker_intervals(input_dir, start, max_elapsed=105)

        self.assertEqual(
            intervals,
            [
                {'start': 0.0, 'end': 75.0, 'phase': 'measure', 'level_vus': 25, 'source': 'marker'},
                {'start': 75.0, 'end': 105.0, 'phase': 'warmup', 'level_vus': 50, 'source': 'marker'},
            ],
        )

    def test_legacy_duplicate_target_csvs_are_grouped_by_panel_name(self):
        with tempfile.TemporaryDirectory() as tmp:
            input_dir = Path(tmp)
            for name in ('Disk Throughput.csv', 'Disk Throughput_2.csv', 'CPU Usage (%).csv', 'HTTP_2xx.csv'):
                (input_dir / name).write_text('timestamp,datetime,elapsed_seconds,value\n')

            groups = collect_metric_csv_groups(input_dir)

        self.assertEqual(
            [(name, [path.name for path in paths]) for name, paths in groups],
            [
                ('CPU Usage (%)', ['CPU Usage (%).csv']),
                ('Disk Throughput', ['Disk Throughput.csv', 'Disk Throughput_2.csv']),
                ('HTTP_2xx', ['HTTP_2xx.csv']),
            ],
        )

    def test_manifest_legend_format_is_used_instead_of_instance_ip(self):
        self.assertEqual(
            legend_label_for_column(
                'Network Throughput',
                'instance=192.168.0.200:9463',
                {'legend_format': '{{instance}} rx'},
            ),
            'Receive',
        )
        self.assertEqual(
            legend_label_for_column(
                'Disk Throughput',
                'instance=192.168.0.200:9463',
                {'legend_format': 'write'},
            ),
            'Write',
        )
        self.assertEqual(
            legend_label_for_column(
                'Disk Throughput',
                'Read',
                {'legend_format': 'write'},
            ),
            'Read',
        )

    def test_throughput_units_are_plotted_as_mb_per_second(self):
        self.assertEqual(conversion_for_metric('Disk Throughput', 'Bps'), (1 / 1_000_000, 'MB/s'))
        self.assertEqual(conversion_for_metric('Per Service Disk write throughput', 'binBps'), (1 / (1024 * 1024), 'MB/s'))


if __name__ == '__main__':
    unittest.main()
