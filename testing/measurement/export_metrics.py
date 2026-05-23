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
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import List, Dict, Any, Optional
import requests


LOADTEST_MARKER_QUERIES = [
    {
        'panel_id': 'loadtest-marker-stage-index',
        'panel_title': 'Load Test Stage Index',
        'query': 'max(last_over_time(k6_loadtest_stage_index[30s]))',
        'legend_format': 'stage',
        'unit': 'short',
        'source': 'loadtest_marker',
    },
    {
        'panel_id': 'loadtest-marker-phase',
        'panel_title': 'Load Test Phase',
        'query': 'max(last_over_time(k6_loadtest_phase_code[30s]))',
        'legend_format': 'phase',
        'unit': 'short',
        'source': 'loadtest_marker',
    },
    {
        'panel_id': 'loadtest-marker-level-vus',
        'panel_title': 'Load Test Level VUs',
        'query': 'max(last_over_time(k6_loadtest_level_vus[30s]))',
        'legend_format': 'devices',
        'unit': 'devices',
        'source': 'loadtest_marker',
    },
    {
        'panel_id': 'loadtest-marker-measurement-window',
        'panel_title': 'Load Test Measurement Window',
        'query': 'max(last_over_time(k6_loadtest_measurement_active[30s]))',
        'legend_format': 'active',
        'unit': 'short',
        'source': 'loadtest_marker',
    },
]


def parse_iso_datetime(iso_string: str) -> float:
    """Convert ISO 8601 datetime string to Unix timestamp."""
    dt = datetime.fromisoformat(iso_string.replace('Z', '+00:00'))
    return dt.timestamp()


def iso_from_timestamp(timestamp: float) -> str:
    """Convert Unix timestamp to ISO 8601 UTC string."""
    return datetime.fromtimestamp(timestamp, timezone.utc).isoformat().replace('+00:00', 'Z')


def extract_dashboard_variables(dashboard: Dict[str, Any]) -> Dict[str, str]:
    """Extract current Grafana template variable values for PromQL substitution."""
    variables = {}
    for item in dashboard.get('templating', {}).get('list', []):
        name = item.get('name')
        if not name:
            continue

        current = item.get('current', {})
        value = current.get('value')
        all_value = item.get('allValue')

        if isinstance(value, list):
            if '$__all' in value or 'All' in value:
                variables[name] = all_value or '.*'
            else:
                variables[name] = '|'.join(str(v) for v in value)
        elif value in ('$__all', 'All'):
            variables[name] = all_value or '.*'
        elif value is not None:
            variables[name] = str(value)
        elif all_value:
            variables[name] = str(all_value)

    return variables


def parse_variable_overrides(overrides: Optional[List[str]]) -> Dict[str, str]:
    """Parse --var key=value overrides."""
    parsed = {}
    for override in overrides or []:
        if '=' not in override:
            raise ValueError(f"Invalid --var value '{override}'. Expected KEY=VALUE.")
        key, value = override.split('=', 1)
        key = key.strip()
        if not key:
            raise ValueError(f"Invalid --var value '{override}'. Variable name is empty.")
        parsed[key] = value.strip()
    return parsed


def substitute_grafana_variables(query: str, variables: Dict[str, str], interval: str) -> str:
    """Replace common Grafana variables with concrete values before calling Prometheus."""
    rendered = query
    built_ins = {
        '__interval': interval,
        '__rate_interval': interval,
    }
    all_variables = {**variables, **built_ins}

    for name, value in all_variables.items():
        rendered = rendered.replace(f'${{{name}}}', value)
        rendered = rendered.replace(f'[[{name}]]', value)
        rendered = re.sub(rf'\${re.escape(name)}\b', value, rendered)

    return rendered


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
                unit = panel.get('fieldConfig', {}).get('defaults', {}).get('unit', '')
                
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
                            'ref_id': target.get('refId', ''),
                            'legend_format': target.get('legendFormat', '__auto'),
                            'unit': unit,
                            'source': 'dashboard',
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


def export_to_csv(data: Dict[str, Any], output_path: str, start_timestamp: float):
    """Export Prometheus query result to CSV file."""
    if data.get('status') != 'success':
        print(f"Warning: Query failed, skipping CSV export for {output_path}", file=sys.stderr)
        return False
    
    result = data.get('data', {}).get('result', [])
    if not result:
        print(f"Warning: No data returned, skipping CSV export for {output_path}", file=sys.stderr)
        return False
    
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
            ts = float(timestamp)
            all_timestamps.add(ts)
            series_data[series_name][ts] = value
    
    # Sort timestamps
    sorted_timestamps = sorted(all_timestamps)
    
    # Write CSV
    with open(output_path, 'w', newline='') as f:
        writer = csv.writer(f)
        # Header row
        header = ['timestamp', 'datetime', 'elapsed_seconds'] + list(series_data.keys())
        writer.writerow(header)
        
        # Data rows
        for ts in sorted_timestamps:
            dt = iso_from_timestamp(ts)
            row = [ts, dt, ts - start_timestamp]
            for series_name in series_data.keys():
                row.append(series_data[series_name].get(ts, ''))
            writer.writerow(row)
    return True


def unique_output_stem(output_dir: Path, title: str, seen: Dict[str, int]) -> Path:
    """Return a stable, unique output stem for a dashboard panel title."""
    safe_name = sanitize_filename(title)
    count = seen.get(safe_name, 0)
    seen[safe_name] = count + 1
    if count:
        safe_name = f"{safe_name}_{count + 1}"
    return output_dir / safe_name


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
    parser.add_argument('--var', dest='variables', action='append',
                        help='Override a Grafana dashboard variable (KEY=VALUE). Can be used multiple times.')
    parser.add_argument('--grafana-interval', default=None,
                        help='Value to use for $__interval and $__rate_interval (default: same as --step)')
    parser.add_argument('--include-loadtest-markers', dest='include_loadtest_markers',
                        action='store_true', default=True,
                        help='Also export k6 load-test marker metrics when present (default: enabled)')
    parser.add_argument('--no-loadtest-markers', dest='include_loadtest_markers',
                        action='store_false',
                        help='Do not export k6 load-test marker metrics')
    
    args = parser.parse_args()
    
    # Convert ISO datetime strings to Unix timestamps
    try:
        start_ts = parse_iso_datetime(args.start_time)
        end_ts = parse_iso_datetime(args.end_time)
    except ValueError as e:
        print(f"Error parsing datetime: {e}", file=sys.stderr)
        sys.exit(1)
    if end_ts <= start_ts:
        print("Error: --to must be after --from", file=sys.stderr)
        sys.exit(1)
    
    # Resolve dashboard path relative to project root
    project_root = Path(__file__).parent.parent.parent
    dashboard_path = project_root / args.dashboard
    
    if not dashboard_path.exists():
        print(f"Error: Dashboard file not found: {dashboard_path}", file=sys.stderr)
        sys.exit(1)
    
    with open(dashboard_path, 'r') as f:
        dashboard = json.load(f)

    try:
        variables = extract_dashboard_variables(dashboard)
        variables.update(parse_variable_overrides(args.variables))
    except ValueError as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

    grafana_interval = args.grafana_interval or args.step

    # Extract queries from dashboard
    print(f"Extracting queries from dashboard: {dashboard_path}")
    queries = extract_queries_from_dashboard(str(dashboard_path))
    if args.include_loadtest_markers:
        queries.extend(LOADTEST_MARKER_QUERIES)
    print(f"Found {len(queries)} Prometheus queries")
    if variables:
        print("Using Grafana variable substitutions:")
        for name, value in sorted(variables.items()):
            print(f"  ${name} = {value}")
    
    # Create output directory
    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    
    # Query Prometheus for each metric
    successful = 0
    failed = 0
    exported_queries = []
    seen_output_names = {}
    
    for i, query_info in enumerate(queries, 1):
        panel_title = query_info['panel_title']
        original_query = query_info['query']
        query = substitute_grafana_variables(original_query, variables, grafana_interval)
        
        print(f"\n[{i}/{len(queries)}] Querying: {panel_title}")
        print(f"  Query: {query}")
        
        # Query Prometheus
        result = query_prometheus(args.prometheus_url, query, start_ts, end_ts, args.step)
        
        if result.get('status') != 'success':
            print(f"  ❌ Failed: {result.get('error', 'Unknown error')}")
            failed += 1
            continue
        
        # Generate output filenames
        base_path = unique_output_stem(output_dir, panel_title, seen_output_names)
        json_path = None
        csv_path = None
        
        # Export to JSON
        if args.format in ['json', 'both']:
            json_path = f"{base_path}.json"
            export_data = {
                'panel_title': panel_title,
                'panel_id': query_info['panel_id'],
                'query': query,
                'original_query': original_query,
                'ref_id': query_info.get('ref_id', ''),
                'legend_format': query_info['legend_format'],
                'unit': query_info.get('unit', ''),
                'source': query_info.get('source', 'dashboard'),
                'time_range': {
                    'from': args.start_time,
                    'to': args.end_time,
                    'from_unix': start_ts,
                    'to_unix': end_ts,
                },
                'prometheus_response': result,
            }
            export_to_json(export_data, json_path)
            print(f"  ✓ Exported JSON: {json_path}")
        
        # Export to CSV
        if args.format in ['csv', 'both']:
            csv_path = f"{base_path}.csv"
            if export_to_csv(result, csv_path, start_ts):
                print(f"  ✓ Exported CSV: {csv_path}")
            else:
                csv_path = None
        
        exported_queries.append({
            'panel_title': panel_title,
            'panel_id': query_info['panel_id'],
            'query': query,
            'original_query': original_query,
            'ref_id': query_info.get('ref_id', ''),
            'legend_format': query_info.get('legend_format', ''),
            'unit': query_info.get('unit', ''),
            'source': query_info.get('source', 'dashboard'),
            'json_file': str(Path(json_path).name) if json_path else None,
            'csv_file': str(Path(csv_path).name) if csv_path else None,
            'series_count': len(result.get('data', {}).get('result', [])),
        })
        successful += 1

    manifest_path = output_dir / 'manifest.json'
    manifest = {
        'exported_at': iso_from_timestamp(datetime.now(timezone.utc).timestamp()),
        'dashboard': str(dashboard_path),
        'prometheus_url': args.prometheus_url,
        'step': args.step,
        'grafana_interval': grafana_interval,
        'variables': variables,
        'time_range': {
            'from': args.start_time,
            'to': args.end_time,
            'from_unix': start_ts,
            'to_unix': end_ts,
        },
        'queries': exported_queries,
    }
    export_to_json(manifest, str(manifest_path))
    print(f"\n✓ Exported manifest: {manifest_path}")
    
    print(f"\n{'='*60}")
    print(f"Export complete!")
    print(f"  Successful: {successful}")
    print(f"  Failed: {failed}")
    print(f"  Output directory: {output_dir.absolute()}")
    print(f"{'='*60}")


if __name__ == '__main__':
    main()
