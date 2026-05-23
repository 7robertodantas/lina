#!/usr/bin/env python3

import csv
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from export_metrics import export_panel_to_csv, format_series_name, group_queries_by_panel


def prometheus_result(metric_labels, values):
    return {
        'status': 'success',
        'data': {
            'result': [
                {
                    'metric': metric_labels,
                    'values': values,
                }
            ]
        },
    }


class ExportMetricsTests(unittest.TestCase):
    def test_groups_dashboard_targets_by_panel(self):
        queries = [
            {'panel_id': 1, 'panel_title': 'Disk Throughput', 'query': 'read'},
            {'panel_id': 1, 'panel_title': 'Disk Throughput', 'query': 'write'},
            {'panel_id': 2, 'panel_title': 'CPU Usage (%)', 'query': 'cpu'},
        ]

        groups = group_queries_by_panel(queries)

        self.assertEqual([group['panel_title'] for group in groups], ['Disk Throughput', 'CPU Usage (%)'])
        self.assertEqual([len(group['queries']) for group in groups], [2, 1])

    def test_renders_meaningful_legend_without_instance(self):
        self.assertEqual(
            format_series_name({'instance': '192.168.0.200:9463'}, '{{instance}} rx', 'Network Throughput', 'A'),
            'Receive',
        )
        self.assertEqual(
            format_series_name({'instance': '192.168.0.200:9463'}, 'write', 'Disk Throughput', 'B'),
            'Write',
        )

    def test_exports_panel_targets_to_one_csv_with_rendered_legends(self):
        values = [[1000, '1048576'], [1015, '2097152']]
        target_results = [
            {
                'panel_title': 'Disk Throughput',
                'panel_id': 3,
                'query': 'read query',
                'original_query': 'read query',
                'ref_id': 'A',
                'legend_format': 'read',
                'unit': 'Bps',
                'source': 'dashboard',
                'prometheus_response': prometheus_result({'instance': '192.168.0.200:9463'}, values),
            },
            {
                'panel_title': 'Disk Throughput',
                'panel_id': 3,
                'query': 'write query',
                'original_query': 'write query',
                'ref_id': 'B',
                'legend_format': 'write',
                'unit': 'Bps',
                'source': 'dashboard',
                'prometheus_response': prometheus_result({'instance': '192.168.0.200:9463'}, values),
            },
        ]

        with tempfile.TemporaryDirectory() as tmp:
            csv_path = Path(tmp) / 'Disk Throughput.csv'
            self.assertTrue(export_panel_to_csv(target_results, str(csv_path), start_timestamp=1000))

            with open(csv_path, newline='') as f:
                rows = list(csv.reader(f))

        self.assertEqual(rows[0], ['timestamp', 'datetime', 'elapsed_seconds', 'Read', 'Write'])
        self.assertEqual(rows[1][2:], ['0.0', '1048576', '1048576'])
        self.assertEqual(rows[2][2:], ['15.0', '2097152', '2097152'])


if __name__ == '__main__':
    unittest.main()
