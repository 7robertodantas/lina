#!/usr/bin/env python3
"""
Export Prometheus metrics from Grafana dashboard to JSON and CSV files.

Usage:
    python export_metrics.py --from "2025-12-18T18:25:04.648Z" --to "2025-12-18T18:34:57.053Z"
    python export_metrics.py --from "2025-12-18T18:25:04.648Z" --to "2025-12-18T18:34:57.053Z" --prometheus-url http://localhost:9090
"""

import argparse
import json
import csv
import os
import sys
from datetime import datetime
from pathlib import Path
from typing import List, Dict, Any
import requests
from urllib.parse import quote


def parse_iso_datetime(iso_string: str) -> float:
    """Convert ISO 8601 datetime string to Unix timestamp."""
    dt = datetime.fromisoformat(iso_string.replace('Z', '+00:00'))
    return dt.timestamp()


def extract_queries_from_dashboard(dashboard_path: str) -> List[Dict[str, Any]]:
    """Extract all Prometheus queries from Grafana dashboard JSON."""
    with open(dashboard_path, 'r') as f:
        dashboard = json.load(f)
    
    queries = []
    panel_id = 0
    
    def extract_from_panels(panels):
        nonlocal panel_id
        for panel in panels:
            if panel.get('type') == 'row':
                # Recursively process nested panels
                if 'panels' in panel:
                    extract_from_panels(panel['panels'])
            elif 'targets' in panel:
                # Check panel-level datasource first, then target-level
                panel_ds = panel.get('datasource', {})
                panel_ds_type = panel_ds.get('type') if isinstance(panel_ds, dict) else None
                
                # Extract queries from targets
                for target in panel.get('targets', []):
                    # Check if this is a Prometheus query
                    target_ds = target.get('datasource', {})
                    target_ds_type = target_ds.get('type') if isinstance(target_ds, dict) else None
                    
                    # Use panel datasource if target doesn't have one
                    datasource_type = target_ds_type or panel_ds_type
                    
                    if 'expr' in target and datasource_type == 'prometheus':
                        query_name = panel.get('title', f'Panel_{panel_id}')
                        queries.append({
                            'panel_id': panel.get('id', panel_id),
                            'panel_title': query_name,
                            'query': target['expr'],
                            'legend_format': target.get('legendFormat', '__auto'),
                        })
            panel_id += 1
    
    extract_from_panels(dashboard.get('panels', []))
    return queries


def sanitize_filename(name: str) -> str:
    """Sanitize a string to be used as a filename."""
    # Replace invalid characters with underscores
    invalid_chars = '<>:"/\\|?*'
    for char in invalid_chars:
        name = name.replace(char, '_')
    # Remove leading/trailing spaces and dots
    name = name.strip('. ')
    return name


def query_prometheus(prometheus_url: str, query: str, start: float, end: float, step: str = '15s') -> Dict[str, Any]:
    """Query Prometheus range API."""
    url = f"{prometheus_url}/api/v1/query_range"
    params = {
        'query': query,
        'start': start,
        'end': end,
        'step': step,
    }
    
    try:
        response = requests.get(url, params=params, timeout=60)
        response.raise_for_status()
        return response.json()
    except requests.exceptions.RequestException as e:
        print(f"Error querying Prometheus: {e}", file=sys.stderr)
        return {'status': 'error', 'error': str(e)}


def export_to_json(data: Dict[str, Any], output_path: str):
    """Export data to JSON file."""
    with open(output_path, 'w') as f:
        json.dump(data, f, indent=2)


def export_to_csv(data: Dict[str, Any], output_path: str):
    """Export Prometheus query result to CSV file."""
    if data.get('status') != 'success':
        print(f"Warning: Query failed, skipping CSV export for {output_path}", file=sys.stderr)
        return
    
    result = data.get('data', {}).get('result', [])
    if not result:
        print(f"Warning: No data returned, skipping CSV export for {output_path}", file=sys.stderr)
        return
    
    # Collect all unique timestamps
    all_timestamps = set()
    series_data = {}
    
    for series in result:
        metric_labels = series.get('metric', {})
        # Create a label string for the series
        if metric_labels:
            label_parts = [f"{k}={v}" for k, v in sorted(metric_labels.items())]
            series_name = ', '.join(label_parts)
        else:
            series_name = 'value'
        
        values = series.get('values', [])
        series_data[series_name] = {}
        
        for timestamp, value in values:
            all_timestamps.add(timestamp)
            series_data[series_name][timestamp] = value
    
    # Sort timestamps
    sorted_timestamps = sorted(all_timestamps)
    
    # Write CSV
    with open(output_path, 'w', newline='') as f:
        writer = csv.writer(f)
        # Header row
        header = ['timestamp', 'datetime'] + list(series_data.keys())
        writer.writerow(header)
        
        # Data rows
        for ts in sorted_timestamps:
            dt = datetime.fromtimestamp(ts).isoformat()
            row = [ts, dt]
            for series_name in series_data.keys():
                row.append(series_data[series_name].get(ts, ''))
            writer.writerow(row)


def main():
    parser = argparse.ArgumentParser(description='Export Prometheus metrics from Grafana dashboard')
    parser.add_argument('--from', dest='start_time', required=True,
                        help='Start time in ISO 8601 format (e.g., 2025-12-18T18:25:04.648Z)')
    parser.add_argument('--to', dest='end_time', required=True,
                        help='End time in ISO 8601 format (e.g., 2025-12-18T18:34:57.053Z)')
    parser.add_argument('--dashboard', default='infrastructure/grafana/dashboards/monitoring.json',
                        help='Path to Grafana dashboard JSON file')
    parser.add_argument('--prometheus-url', default='http://localhost:9090',
                        help='Prometheus server URL')
    parser.add_argument('--output-dir', default='metrics_export',
                        help='Output directory for exported files')
    parser.add_argument('--step', default='15s',
                        help='Query resolution step width (e.g., 15s, 1m)')
    parser.add_argument('--format', choices=['json', 'csv', 'both'], default='both',
                        help='Output format: json, csv, or both')
    
    args = parser.parse_args()
    
    # Convert ISO datetime strings to Unix timestamps
    try:
        start_ts = parse_iso_datetime(args.start_time)
        end_ts = parse_iso_datetime(args.end_time)
    except ValueError as e:
        print(f"Error parsing datetime: {e}", file=sys.stderr)
        sys.exit(1)
    
    # Resolve dashboard path relative to project root
    project_root = Path(__file__).parent.parent.parent
    dashboard_path = project_root / args.dashboard
    
    if not dashboard_path.exists():
        print(f"Error: Dashboard file not found: {dashboard_path}", file=sys.stderr)
        sys.exit(1)
    
    # Extract queries from dashboard
    print(f"Extracting queries from dashboard: {dashboard_path}")
    queries = extract_queries_from_dashboard(str(dashboard_path))
    print(f"Found {len(queries)} Prometheus queries")
    
    # Create output directory
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Query Prometheus for each metric
    successful = 0
    failed = 0
    
    for i, query_info in enumerate(queries, 1):
        panel_title = query_info['panel_title']
        query = query_info['query']
        
        print(f"\n[{i}/{len(queries)}] Querying: {panel_title}")
        print(f"  Query: {query}")
        
        # Query Prometheus
        result = query_prometheus(args.prometheus_url, query, start_ts, end_ts, args.step)
        
        if result.get('status') != 'success':
            print(f"  ❌ Failed: {result.get('error', 'Unknown error')}")
            failed += 1
            continue
        
        # Generate output filenames
        safe_name = sanitize_filename(panel_title)
        base_path = output_dir / safe_name
        
        # Export to JSON
        if args.format in ['json', 'both']:
            json_path = f"{base_path}.json"
            export_data = {
                'panel_title': panel_title,
                'panel_id': query_info['panel_id'],
                'query': query,
                'legend_format': query_info['legend_format'],
                'time_range': {
                    'from': args.start_time,
                    'to': args.end_time,
                },
                'prometheus_response': result,
            }
            export_to_json(export_data, json_path)
            print(f"  ✓ Exported JSON: {json_path}")
        
        # Export to CSV
        if args.format in ['csv', 'both']:
            csv_path = f"{base_path}.csv"
            export_to_csv(result, csv_path)
            print(f"  ✓ Exported CSV: {csv_path}")
        
        successful += 1
    
    print(f"\n{'='*60}")
    print(f"Export complete!")
    print(f"  Successful: {successful}")
    print(f"  Failed: {failed}")
    print(f"  Output directory: {output_dir.absolute()}")
    print(f"{'='*60}")


if __name__ == '__main__':
    main()

