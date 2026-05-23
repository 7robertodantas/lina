#!/usr/bin/env python3
"""
Plot metrics exported by export_metrics.py with experiment-relative time.

The graphs are intended for load-test reports: the x-axis is elapsed time, load
levels are shaded directly on the plot, and teardown is called out so the final
decrease is not mistaken for recovery during a measurement window.

Usage:
    python plot_exported_metrics.py
    python plot_exported_metrics.py --input-dir metrics_export --output-dir graphs
"""

import argparse
import json
import os
import re
import tempfile
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional

plot_cache_dir = Path(tempfile.gettempdir()) / 'lnpay_plot_cache'
(plot_cache_dir / 'matplotlib').mkdir(parents=True, exist_ok=True)
(plot_cache_dir / 'xdg').mkdir(parents=True, exist_ok=True)
os.environ.setdefault('MPLCONFIGDIR', str(plot_cache_dir / 'matplotlib'))
os.environ.setdefault('XDG_CACHE_HOME', str(plot_cache_dir / 'xdg'))

import matplotlib

matplotlib.use('Agg')
import matplotlib.pyplot as plt
import pandas as pd
import seaborn as sns


DEFAULT_INPUT_DIR = 'metrics_export'
DEFAULT_OUTPUT_DIR = 'graphs'
DEFAULT_DASHBOARD = 'infrastructure/grafana/dashboards/monitoring.json'

MARKER_TITLES = {
    'Load Test Stage Index',
    'Load Test Phase',
    'Load Test Level VUs',
    'Load Test Measurement Window',
}

PHASE_BY_CODE = {
    0: 'idle',
    1: 'warmup',
    2: 'measure',
    3: 'teardown',
}

PHASE_STYLES = {
    'warmup': {'color': '#f4a261', 'alpha': 0.12, 'label': 'Warmup'},
    'measure': {'color': '#2a9d8f', 'alpha': 0.14, 'label': 'Measurement'},
    'teardown': {'color': '#6c757d', 'alpha': 0.16, 'label': 'Teardown'},
}

UNIT_LABELS = {
    'Bps': 'Bytes/s',
    'binBps': 'Bytes/s',
    'bytes': 'Bytes',
    'decbytes': 'Bytes',
    'debits/s': 'debits/s',
    'devices': 'devices',
    'eps': 'events/s',
    'events': 'events',
    'percent': 'Percent (%)',
    's': 'Seconds',
    'short': 'Value',
}

METADATA_COLUMNS = {
    'timestamp',
    'datetime',
    'elapsed_seconds',
    'datetime_parsed',
    'elapsed_from_experiment_start',
    'elapsed_minutes',
}

sns.set_theme(style='whitegrid')


def sanitize_filename(name: str) -> str:
    invalid_chars = '<>:"/\\|?*'
    for char in invalid_chars:
        name = name.replace(char, '_')
    return name.strip('. ')


def project_path(path: str) -> Path:
    candidate = Path(path)
    if candidate.is_absolute():
        return candidate
    return Path(__file__).parent.parent.parent / candidate


def parse_duration_seconds(value: str) -> float:
    """Parse k6-style durations such as 60s, 2m, or 1m30s."""
    total = 0.0
    matches = re.findall(r'(\d+(?:\.\d+)?)(ms|s|m|h)', value)
    if not matches:
        raise ValueError(f"Invalid duration '{value}'")

    for amount_raw, unit in matches:
        amount = float(amount_raw)
        if unit == 'ms':
            total += amount / 1000
        elif unit == 's':
            total += amount
        elif unit == 'm':
            total += amount * 60
        elif unit == 'h':
            total += amount * 3600

    return total


def load_manifest(input_dir: Path) -> Dict[str, Any]:
    manifest_path = input_dir / 'manifest.json'
    if not manifest_path.exists():
        return {}
    try:
        with open(manifest_path, 'r') as f:
            return json.load(f)
    except Exception as exc:
        print(f"Warning: Could not read manifest.json: {exc}")
        return {}


def load_dashboard_units(dashboard_path: str) -> Dict[str, str]:
    """Load unit mappings from Grafana dashboard JSON."""
    dashboard_file = project_path(dashboard_path)
    if not dashboard_file.exists():
        print(f"Warning: Dashboard file not found: {dashboard_file}")
        return {}

    try:
        with open(dashboard_file, 'r') as f:
            dashboard = json.load(f)
    except Exception as exc:
        print(f"Warning: Could not load dashboard units: {exc}")
        return {}

    units_map = {}

    def extract_panels(panels: Iterable[Dict[str, Any]]):
        for panel in panels:
            if panel.get('type') == 'row':
                extract_panels(panel.get('panels', []))
                continue
            title = panel.get('title', '')
            unit = panel.get('fieldConfig', {}).get('defaults', {}).get('unit', '')
            if title and unit:
                units_map[title] = unit

    extract_panels(dashboard.get('panels', []))
    return units_map


def load_units(input_dir: Path, dashboard_path: str) -> Dict[str, str]:
    units = load_dashboard_units(dashboard_path)
    manifest = load_manifest(input_dir)
    for query in manifest.get('queries', []):
        csv_file = query.get('csv_file')
        unit = query.get('unit')
        if csv_file and unit:
            units[Path(csv_file).stem] = unit
    return units


def get_unit_label(unit_code: str) -> str:
    return UNIT_LABELS.get(unit_code, unit_code or 'Value')


def find_metric_csv(input_dir: Path, title: str) -> Optional[Path]:
    candidates = [
        input_dir / f'{title}.csv',
        input_dir / f'{sanitize_filename(title)}.csv',
    ]
    for candidate in candidates:
        if candidate.exists():
            return candidate

    globbed = sorted(input_dir.glob(f'{sanitize_filename(title)}*.csv'))
    return globbed[0] if globbed else None


def read_metric_csv(csv_path: Path) -> Optional[pd.DataFrame]:
    try:
        df = pd.read_csv(csv_path)
    except Exception as exc:
        print(f"  Error reading {csv_path.name}: {exc}")
        return None

    if df.empty:
        return None

    if 'datetime' in df.columns:
        df['datetime_parsed'] = pd.to_datetime(df['datetime'], utc=True, errors='coerce')
    elif 'timestamp' in df.columns:
        df['datetime_parsed'] = pd.to_datetime(df['timestamp'], unit='s', utc=True, errors='coerce')
    else:
        return None

    df = df[df['datetime_parsed'].notna()].copy()
    if df.empty:
        return None

    return df.sort_values('datetime_parsed')


def coerce_metric_columns(df: pd.DataFrame) -> List[str]:
    columns = []
    for column in df.columns:
        if column in METADATA_COLUMNS or column.endswith('_min'):
            continue
        numeric = pd.to_numeric(df[column], errors='coerce')
        if numeric.notna().any():
            df[column] = numeric
            columns.append(column)
    return columns


def load_single_series(input_dir: Path, title: str) -> Optional[pd.DataFrame]:
    csv_path = find_metric_csv(input_dir, title)
    if not csv_path:
        return None

    df = read_metric_csv(csv_path)
    if df is None:
        return None

    data_cols = coerce_metric_columns(df)
    if not data_cols:
        return None

    return df[['datetime_parsed', data_cols[0]]].rename(columns={data_cols[0]: 'value'})


def find_initial_time(input_dir: Path) -> Optional[pd.Timestamp]:
    """Prefer explicit load-test markers, then fall back to the first nonzero VU sample."""
    for marker_title in ('Load Test Phase', 'Load Test Level VUs'):
        marker = load_single_series(input_dir, marker_title)
        if marker is not None and not marker.empty:
            return marker['datetime_parsed'].iloc[0]

    for vu_title in ('Virtual Users over time', "VU's"):
        csv_path = find_metric_csv(input_dir, vu_title)
        if not csv_path:
            continue
        df = read_metric_csv(csv_path)
        if df is None:
            continue
        data_cols = coerce_metric_columns(df)
        if not data_cols:
            continue
        active = df[df[data_cols].max(axis=1) > 0]
        if not active.empty:
            return active['datetime_parsed'].iloc[0]

    return None


def find_max_elapsed(input_dir: Path, initial_time: Optional[pd.Timestamp]) -> float:
    if initial_time is None:
        return 0.0

    manifest = load_manifest(input_dir)
    end_unix = manifest.get('time_range', {}).get('to_unix')
    if end_unix is not None:
        end_time = pd.to_datetime(float(end_unix), unit='s', utc=True)
        return max(0.0, (end_time - initial_time).total_seconds())

    max_time = initial_time
    for csv_path in input_dir.glob('*.csv'):
        df = read_metric_csv(csv_path)
        if df is not None and not df.empty:
            max_time = max(max_time, df['datetime_parsed'].max())
    return max(0.0, (max_time - initial_time).total_seconds())


def merge_intervals(intervals: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    merged = []
    for interval in intervals:
        if interval['end'] <= interval['start'] or interval['phase'] == 'idle':
            continue
        if (
            merged
            and merged[-1]['phase'] == interval['phase']
            and int(merged[-1]['level_vus']) == int(interval['level_vus'])
            and abs(merged[-1]['end'] - interval['start']) <= 1e-6
        ):
            merged[-1]['end'] = interval['end']
            continue
        merged.append(interval)
    return merged


def align_teardown_transition_segments(intervals: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    aligned = []
    for index, interval in enumerate(intervals):
        adjusted = dict(interval)
        next_interval = intervals[index + 1] if index + 1 < len(intervals) else None

        # The plotted VU line connects samples, so the segment ending at the
        # first teardown sample is the visible ramp-down segment.
        if next_interval and adjusted['phase'] not in {'idle', 'teardown'} and next_interval['phase'] == 'teardown':
            adjusted['phase'] = 'teardown'

        if adjusted['phase'] == 'teardown':
            adjusted['level_vus'] = 0

        aligned.append(adjusted)

    return aligned


def build_marker_intervals(input_dir: Path, initial_time: pd.Timestamp, max_elapsed: float) -> List[Dict[str, Any]]:
    phase = load_single_series(input_dir, 'Load Test Phase')
    level = load_single_series(input_dir, 'Load Test Level VUs')
    measurement = load_single_series(input_dir, 'Load Test Measurement Window')
    if phase is None or level is None or phase.empty or level.empty:
        return []

    timeline = pd.merge_asof(
        phase.sort_values('datetime_parsed').rename(columns={'value': 'phase_code'}),
        level.sort_values('datetime_parsed').rename(columns={'value': 'level_vus'}),
        on='datetime_parsed',
        direction='nearest',
    )

    if measurement is not None and not measurement.empty:
        timeline = pd.merge_asof(
            timeline.sort_values('datetime_parsed'),
            measurement.sort_values('datetime_parsed').rename(columns={'value': 'measurement_active'}),
            on='datetime_parsed',
            direction='nearest',
        )
    else:
        timeline['measurement_active'] = 0

    timeline['elapsed'] = (timeline['datetime_parsed'] - initial_time).dt.total_seconds()
    timeline = timeline[(timeline['elapsed'] >= 0) & (timeline['elapsed'] <= max_elapsed)].copy()
    if timeline.empty:
        return []

    intervals = []
    elapsed_values = timeline['elapsed'].tolist()
    rows = timeline.to_dict('records')

    for index, row in enumerate(rows):
        start = 0.0 if index == 0 and row['elapsed'] < 1 else float(row['elapsed'])
        end = float(elapsed_values[index + 1]) if index + 1 < len(elapsed_values) else max_elapsed
        phase_code = int(round(row.get('phase_code', -1)))
        phase_name = PHASE_BY_CODE.get(phase_code, 'unknown')
        # The measurement gauge is exported through a range query and can lag by
        # one step, so only use it as a fallback when phase_code is unavailable.
        if phase_name == 'unknown' and row.get('measurement_active', 0) >= 0.5:
            phase_name = 'measure'
        intervals.append({
            'start': start,
            'end': end,
            'phase': phase_name,
            'level_vus': max(0, int(round(row.get('level_vus', 0) or 0))),
            'source': 'marker',
        })

    return merge_intervals(align_teardown_transition_segments(intervals))


def build_schedule_intervals(
    max_elapsed: float,
    level_vus: int,
    level_count: int,
    warmup_seconds: float,
    measure_seconds: float,
) -> List[Dict[str, Any]]:
    intervals = []
    cursor = 0.0
    for level in range(1, level_count + 1):
        target_vus = level * level_vus
        warmup_end = min(max_elapsed, cursor + warmup_seconds)
        intervals.append({
            'start': cursor,
            'end': warmup_end,
            'phase': 'warmup',
            'level_vus': target_vus,
            'source': 'schedule',
        })

        measure_start = cursor + warmup_seconds
        measure_end = min(max_elapsed, measure_start + measure_seconds)
        intervals.append({
            'start': measure_start,
            'end': measure_end,
            'phase': 'measure',
            'level_vus': target_vus,
            'source': 'schedule',
        })

        cursor += warmup_seconds + measure_seconds
        if cursor >= max_elapsed:
            break

    if max_elapsed > cursor:
        intervals.append({
            'start': cursor,
            'end': max_elapsed,
            'phase': 'teardown',
            'level_vus': 0,
            'source': 'schedule',
        })

    return merge_intervals(intervals)


def x_value(seconds: float, use_minutes: bool) -> float:
    return seconds / 60 if use_minutes else seconds


def annotate_phases(ax, intervals: List[Dict[str, Any]], use_minutes: bool):
    if not intervals:
        return

    used_phases = set()
    for interval in intervals:
        phase = interval['phase']
        style = PHASE_STYLES.get(phase)
        if not style:
            continue
        start = x_value(interval['start'], use_minutes)
        end = x_value(interval['end'], use_minutes)
        label = style['label'] if phase not in used_phases else '_nolegend_'
        ax.axvspan(start, end, color=style['color'], alpha=style['alpha'], linewidth=0, label=label)
        used_phases.add(phase)

        width_seconds = interval['end'] - interval['start']
        if phase == 'measure' and width_seconds >= 30:
            ax.text(
                (start + end) / 2,
                0.98,
                f"{interval['level_vus']} devices\nmeasure",
                transform=ax.get_xaxis_transform(),
                ha='center',
                va='top',
                fontsize=8,
                color='#0f5132',
            )
        elif phase == 'teardown' and width_seconds >= 10:
            ax.text(
                (start + end) / 2,
                0.98,
                'teardown',
                transform=ax.get_xaxis_transform(),
                ha='center',
                va='top',
                fontsize=8,
                color='#343a40',
            )


def add_expected_rate_overlay(
    ax,
    metric_name: str,
    intervals: List[Dict[str, Any]],
    use_minutes: bool,
    report_interval_seconds: float,
):
    if report_interval_seconds <= 0:
        return

    lower_name = metric_name.lower()
    if not (
        'consumption recorded' in lower_name
        or 'authorization debits' in lower_name
        or 'mqtt events received' in lower_name
        or 'mqtt events processed' in lower_name
    ):
        return

    points_x = []
    points_y = []
    for interval in intervals:
        if interval['phase'] not in {'warmup', 'measure'}:
            continue
        expected = interval['level_vus'] / report_interval_seconds
        points_x.extend([x_value(interval['start'], use_minutes), x_value(interval['end'], use_minutes)])
        points_y.extend([expected, expected])

    if points_x:
        ax.step(
            points_x,
            points_y,
            where='post',
            color='#111827',
            linestyle='--',
            linewidth=1.2,
            label='Incoming report rate',
        )


def add_saturation_thresholds(
    ax,
    metric_name: str,
    ylabel: str,
    resource_threshold_percent: float,
    latency_threshold_seconds: float,
):
    lower_name = metric_name.lower()
    if 'percent' in ylabel.lower() or '(%)' in metric_name or 'usage (%)' in lower_name:
        ax.axhline(resource_threshold_percent, color='#b00020', linestyle=':', linewidth=1.2)
        ax.text(
            0.995,
            resource_threshold_percent,
            f'{resource_threshold_percent:g}% saturation',
            transform=ax.get_yaxis_transform(),
            ha='right',
            va='bottom',
            fontsize=8,
            color='#b00020',
        )

    if 'latency' in lower_name and latency_threshold_seconds > 0:
        ax.axhline(latency_threshold_seconds, color='#b00020', linestyle=':', linewidth=1.2)
        ax.text(
            0.995,
            latency_threshold_seconds,
            f'{latency_threshold_seconds:g}s threshold',
            transform=ax.get_yaxis_transform(),
            ha='right',
            va='bottom',
            fontsize=8,
            color='#b00020',
        )


def clean_series_label(metric_name: str, column: str) -> str:
    if column == 'value':
        return metric_name
    for key in ('name=', 'instance=', 'topic=', 'stream=', 'group='):
        if key in column:
            return column.split(key, 1)[1].split(',', 1)[0].strip()
    return column


def conversion_for_metric(metric_name: str, unit_code: str) -> tuple[float, str]:
    lower_name = metric_name.lower()
    if unit_code in {'bytes', 'decbytes'} and 'memory' in lower_name:
        return 1 / (1024 * 1024), 'MiB'
    if unit_code in {'Bps', 'binBps'}:
        return 1 / 1024, 'KiB/s'
    return 1.0, get_unit_label(unit_code)


def plot_csv_file(
    csv_path: Path,
    output_dir: Path,
    units_map: Dict[str, str],
    initial_time: Optional[pd.Timestamp],
    intervals: List[Dict[str, Any]],
    report_interval_seconds: float,
    resource_threshold_percent: float,
    latency_threshold_seconds: float,
) -> bool:
    metric_name = csv_path.stem
    if metric_name in MARKER_TITLES:
        return False

    print(f"Processing: {csv_path.name}")
    df = read_metric_csv(csv_path)
    if df is None:
        print("  No valid timestamped data")
        return False

    data_cols = coerce_metric_columns(df)
    if not data_cols:
        print("  No numeric data columns")
        return False

    if initial_time is not None:
        df['elapsed_from_experiment_start'] = (df['datetime_parsed'] - initial_time).dt.total_seconds()
        df = df[df['elapsed_from_experiment_start'] >= 0].copy()
        if df.empty:
            print("  No samples after experiment start")
            return False
        time_col = 'elapsed_from_experiment_start'
        xlabel_prefix = 'Elapsed time'
    elif 'elapsed_seconds' in df.columns:
        time_col = 'elapsed_seconds'
        xlabel_prefix = 'Elapsed time from export start'
    else:
        print("  Could not determine relative time")
        return False

    unit_code = units_map.get(metric_name, '')
    conversion_factor, ylabel = conversion_for_metric(metric_name, unit_code)
    if conversion_factor != 1.0:
        for col in data_cols:
            df[col] = df[col] * conversion_factor

    max_elapsed = float(df[time_col].max())
    use_minutes = max_elapsed > 600
    if use_minutes:
        df['elapsed_minutes'] = df[time_col] / 60
        x_col = 'elapsed_minutes'
        xlabel = f'{xlabel_prefix} (min)'
    else:
        x_col = time_col
        xlabel = f'{xlabel_prefix} (s)'

    fig, ax = plt.subplots(figsize=(13, 6))

    annotate_phases(ax, intervals, use_minutes)

    for col in data_cols:
        ax.plot(
            df[x_col],
            df[col],
            label=clean_series_label(metric_name, col),
            linewidth=1.5,
            alpha=0.9,
        )

    add_expected_rate_overlay(ax, metric_name, intervals, use_minutes, report_interval_seconds)
    add_saturation_thresholds(ax, metric_name, ylabel, resource_threshold_percent, latency_threshold_seconds)

    ax.set_title(metric_name, fontsize=16, fontweight='bold', pad=18)
    ax.set_xlabel(xlabel, fontsize=12, fontweight='bold')
    ax.set_ylabel(ylabel, fontsize=12, fontweight='bold')
    ax.grid(True, linestyle='--', alpha=0.55)
    ax.set_xlim(float(df[x_col].min()), float(df[x_col].max()))

    if use_minutes:
        ax.xaxis.set_major_locator(plt.MultipleLocator(2))
    elif max_elapsed <= 300:
        ax.xaxis.set_major_locator(plt.MultipleLocator(30))
    else:
        ax.xaxis.set_major_locator(plt.MultipleLocator(60))

    handles, labels = ax.get_legend_handles_labels()
    if handles:
        ax.legend(
            handles,
            labels,
            bbox_to_anchor=(1.02, 1),
            loc='upper left',
            borderaxespad=0,
            title='Series / phase',
        )

    output_path = output_dir / f'{sanitize_filename(metric_name)}.png'
    fig.tight_layout()
    fig.savefig(output_path, dpi=300)
    plt.close(fig)
    print(f"  Saved: {output_path}")
    return True


def generate_graphs(
    input_dir: str = DEFAULT_INPUT_DIR,
    output_dir: str = DEFAULT_OUTPUT_DIR,
    dashboard_path: str = DEFAULT_DASHBOARD,
    level_vus: int = 25,
    level_count: int = 8,
    warmup: str = '60s',
    measure: str = '120s',
    report_interval_seconds: float = 1.0,
    resource_threshold_percent: float = 90.0,
    latency_threshold_seconds: float = 5.0,
):
    input_path = Path(input_dir)
    output_path = Path(output_dir)

    if not input_path.exists():
        print(f"Error: Input directory '{input_dir}' not found.")
        print("Run export_metrics.py first to generate CSV files.")
        return

    units_map = load_units(input_path, dashboard_path)
    output_path.mkdir(parents=True, exist_ok=True)

    initial_time = find_initial_time(input_path)
    if initial_time is None:
        print("Warning: Could not find experiment start; using export-relative elapsed seconds where available.")
        intervals = []
    else:
        print(f"Experiment start: {initial_time.isoformat()}")
        max_elapsed = find_max_elapsed(input_path, initial_time)
        intervals = build_marker_intervals(input_path, initial_time, max_elapsed)
        if intervals:
            print(f"Using load-test marker metrics for {len(intervals)} timeline intervals.")
        else:
            warmup_seconds = parse_duration_seconds(warmup)
            measure_seconds = parse_duration_seconds(measure)
            intervals = build_schedule_intervals(max_elapsed, level_vus, level_count, warmup_seconds, measure_seconds)
            print(f"Using configured schedule fallback for {len(intervals)} timeline intervals.")

    csv_files = sorted(path for path in input_path.glob('*.csv') if path.stem not in MARKER_TITLES)
    if not csv_files:
        print(f"No metric CSV files found in '{input_dir}'")
        return

    print(f"Found {len(csv_files)} plottable CSV files in '{input_dir}'")
    print(f"Output directory: '{output_dir}'")
    print()

    successful = 0
    failed = 0
    for csv_file in csv_files:
        if plot_csv_file(
            csv_file,
            output_path,
            units_map,
            initial_time,
            intervals,
            report_interval_seconds,
            resource_threshold_percent,
            latency_threshold_seconds,
        ):
            successful += 1
        else:
            failed += 1

    print()
    print(f"{'=' * 60}")
    print("Plotting complete!")
    print(f"  Successful: {successful}")
    print(f"  Skipped/failed: {failed}")
    print(f"  Output directory: {output_path.absolute()}")
    print(f"{'=' * 60}")


def main():
    parser = argparse.ArgumentParser(
        description='Plot exported metrics with load-level and teardown annotations',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python plot_exported_metrics.py
  python plot_exported_metrics.py --input-dir metrics_export --output-dir graphs
  python plot_exported_metrics.py --level-vus 25 --level-count 8 --warmup 60s --measure 120s
        """,
    )
    parser.add_argument('--input-dir', default=DEFAULT_INPUT_DIR,
                        help=f'Input directory containing CSV files (default: {DEFAULT_INPUT_DIR})')
    parser.add_argument('--output-dir', default=DEFAULT_OUTPUT_DIR,
                        help=f'Output directory for PNG files (default: {DEFAULT_OUTPUT_DIR})')
    parser.add_argument('--dashboard', default=DEFAULT_DASHBOARD,
                        help=f'Path to Grafana dashboard JSON (default: {DEFAULT_DASHBOARD})')
    parser.add_argument('--level-vus', type=int, default=25,
                        help='Number of devices added per load level when marker metrics are unavailable')
    parser.add_argument('--level-count', type=int, default=8,
                        help='Number of configured load levels when marker metrics are unavailable')
    parser.add_argument('--warmup', default='60s',
                        help='Warmup duration per level when marker metrics are unavailable')
    parser.add_argument('--measure', default='120s',
                        help='Measurement duration per level when marker metrics are unavailable')
    parser.add_argument('--report-interval-seconds', type=float, default=1.0,
                        help='Expected usage report interval per device, used for incoming-rate overlays')
    parser.add_argument('--resource-threshold-percent', type=float, default=90.0,
                        help='Saturation threshold line for percent resource metrics')
    parser.add_argument('--latency-threshold-seconds', type=float, default=5.0,
                        help='Saturation threshold line for latency metrics')

    args = parser.parse_args()
    generate_graphs(
        input_dir=args.input_dir,
        output_dir=args.output_dir,
        dashboard_path=args.dashboard,
        level_vus=args.level_vus,
        level_count=args.level_count,
        warmup=args.warmup,
        measure=args.measure,
        report_interval_seconds=args.report_interval_seconds,
        resource_threshold_percent=args.resource_threshold_percent,
        latency_threshold_seconds=args.latency_threshold_seconds,
    )


if __name__ == '__main__':
    main()
