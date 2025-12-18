#!/usr/bin/env python3
"""
Plot metrics from exported CSV files (from export_metrics.py).

This script reads all CSV files from the metrics export directory and generates
matplotlib graphs in the same style as plot_graphs.py.

Usage:
    python plot_exported_metrics.py
    python plot_exported_metrics.py --input-dir metrics_export --output-dir graphs
"""

import pandas as pd
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
import seaborn as sns
import os
import argparse
import json
from pathlib import Path
from datetime import datetime


# --- CONFIGURATION ---
DEFAULT_INPUT_DIR = 'metrics_export'
DEFAULT_OUTPUT_DIR = 'graphs'
DEFAULT_DASHBOARD = 'infrastructure/grafana/dashboards/monitoring.json'
sns.set_theme(style="whitegrid")  # Academic/Clean style


# Map Grafana unit codes to human-readable labels
UNIT_LABELS = {
    'binBps': 'Bytes/s',  # Binary bytes per second (auto-formats to KiB/s, MiB/s, etc.)
    'decbytes': 'Bytes',  # Decimal bytes (auto-formats to KB, MB, etc.)
    'bytes': 'Bytes',
    'debits/s': 'debits/s',
    's': 'Seconds',
    'percent': 'Percent %',
    'eps': 'events/s',
    'events': 'events',
    'devices': 'devices',
    'cores': 'cores',
}


def parse_datetime(dt_str):
    """Parse ISO datetime string to datetime object."""
    try:
        # Remove 'Z' if present and replace with timezone offset
        dt_str_clean = dt_str.replace('Z', '+00:00')
        # Parse ISO format datetime
        return datetime.fromisoformat(dt_str_clean)
    except Exception as e:
        # Fallback: try pandas parsing
        try:
            return pd.to_datetime(dt_str).to_pydatetime()
        except:
            return None


def format_datetime_for_axis(dt):
    """Format datetime for x-axis display."""
    return dt.strftime('%H:%M:%S')


def sanitize_filename(name: str) -> str:
    """Sanitize a string to be used as a filename."""
    invalid_chars = '<>:"/\\|?*'
    for char in invalid_chars:
        name = name.replace(char, '_')
    name = name.strip('. ')
    return name


def load_dashboard_units(dashboard_path: str) -> dict:
    """Load unit mappings from Grafana dashboard JSON."""
    try:
        dashboard_file = Path(dashboard_path)
        if not dashboard_file.is_absolute():
            # Try relative to project root
            project_root = Path(__file__).parent.parent.parent
            dashboard_file = project_root / dashboard_path
        
        if not dashboard_file.exists():
            print(f"Warning: Dashboard file not found: {dashboard_file}")
            return {}
        
        with open(dashboard_file, 'r') as f:
            dashboard = json.load(f)
        
        units_map = {}
        
        def extract_panels(panels):
            for panel in panels:
                if panel.get('type') == 'row':
                    if 'panels' in panel:
                        extract_panels(panel['panels'])
                elif 'targets' in panel:
                    title = panel.get('title', '')
                    unit = panel.get('fieldConfig', {}).get('defaults', {}).get('unit', '')
                    if title and unit:
                        units_map[title] = unit
        
        extract_panels(dashboard.get('panels', []))
        return units_map
    
    except Exception as e:
        print(f"Warning: Could not load dashboard units: {e}")
        return {}


def get_unit_label(unit_code: str) -> str:
    """Convert Grafana unit code to human-readable label."""
    return UNIT_LABELS.get(unit_code, unit_code)


def find_initial_time(input_dir: Path) -> pd.Timestamp:
    """Find the initial time when VU first becomes > 0."""
    vu_csv = input_dir / 'Virtual Users over time.csv'
    if not vu_csv.exists():
        # Fallback: try VU's.csv
        vu_csv = input_dir / "VU's.csv"
        if not vu_csv.exists():
            return None
    
    try:
        df_vu = pd.read_csv(vu_csv)
        if 'datetime' in df_vu.columns:
            df_vu['datetime_parsed'] = pd.to_datetime(df_vu['datetime'])
        elif 'timestamp' in df_vu.columns:
            df_vu['datetime_parsed'] = pd.to_datetime(df_vu['timestamp'], unit='s')
        else:
            return None
        
        # Find first time when VU > 0
        data_col = 'value' if 'value' in df_vu.columns else df_vu.columns[2]  # Assume 3rd column is VU
        first_vu = df_vu[df_vu[data_col] > 0]
        if not first_vu.empty:
            return first_vu['datetime_parsed'].iloc[0]
    except Exception as e:
        print(f"Warning: Could not find initial time: {e}")
    
    return None


def identify_vu_levels(input_dir: Path, initial_time: pd.Timestamp) -> list:
    """Identify VU level boundaries (every 90s, increments of 25)."""
    if initial_time is None:
        return []
    
    vu_csv = input_dir / 'Virtual Users over time.csv'
    if not vu_csv.exists():
        vu_csv = input_dir / "VU's.csv"
        if not vu_csv.exists():
            return []
    
    try:
        df_vu = pd.read_csv(vu_csv)
        if 'datetime' in df_vu.columns:
            df_vu['datetime_parsed'] = pd.to_datetime(df_vu['datetime'])
        elif 'timestamp' in df_vu.columns:
            df_vu['datetime_parsed'] = pd.to_datetime(df_vu['timestamp'], unit='s')
        else:
            return []
        
        data_col = 'value' if 'value' in df_vu.columns else df_vu.columns[2]
        df_vu['elapsed'] = (df_vu['datetime_parsed'] - initial_time).dt.total_seconds()
        df_vu = df_vu[df_vu['elapsed'] >= 0].copy()
        
        if df_vu.empty:
            return []
        
        # Identify level boundaries (every 90s)
        # Pattern: WARMUP (30s) + MEASURE (60s) = 90s per level
        # Each period: 0-30s WARMUP, 30-90s MEASURE
        levels = []
        max_elapsed = df_vu['elapsed'].max()
        
        # Find VU level for each 90s period by looking at MEASURE phase (30-90s of each period)
        period = 0
        while True:
            period_start = period * 90
            measure_start = period_start + 30  # MEASURE phase starts at 30s into period
            measure_end = period_start + 90    # MEASURE phase ends at 90s (end of period)
            
            if measure_start > max_elapsed:
                break
            
            # Get VU values during MEASURE phase of this period
            measure_data = df_vu[(df_vu['elapsed'] >= measure_start) & (df_vu['elapsed'] < measure_end)]
            
            if not measure_data.empty:
                # Use median or most common VU value during MEASURE phase
                vu_values = measure_data[data_col].values
                # Round to nearest 25
                vu_level = round(vu_values.mean() / 25) * 25
                
                # Mark boundary at the end of this period (start of next period)
                boundary_time = measure_end
                if boundary_time <= max_elapsed:
                    levels.append({
                        'time': boundary_time,
                        'vu_level': int(vu_level)
                    })
            
            period += 1
        
        return levels
    except Exception as e:
        print(f"Warning: Could not identify VU levels: {e}")
        return []


def plot_csv_file(csv_path: Path, output_dir: Path, units_map: dict, initial_time: pd.Timestamp = None, vu_levels: list = None):
    """Plot a single CSV file."""
    print(f"Processing: {csv_path.name}")
    
    try:
        df = pd.read_csv(csv_path)
    except Exception as e:
        print(f"  ❌ Error reading CSV: {e}")
        return False
    
    if df.empty:
        print(f"  ⚠️  CSV file is empty")
        return False
    
    # Parse datetime column
    if 'datetime' in df.columns:
        # Use pandas to_datetime for better compatibility with matplotlib
        df['datetime_parsed'] = pd.to_datetime(df['datetime'])
        # Remove rows where datetime parsing failed
        df = df[df['datetime_parsed'].notna()].copy()
        if df.empty:
            print(f"  ⚠️  No valid datetime data")
            return False
    elif 'timestamp' in df.columns:
        # Fallback to timestamp if datetime not available
        df['datetime_parsed'] = pd.to_datetime(df['timestamp'], unit='s')
    else:
        print(f"  ❌ No timestamp or datetime column found")
        return False
    
    # Convert to elapsed time if initial_time is provided
    if initial_time is not None:
        df['elapsed_seconds'] = (df['datetime_parsed'] - initial_time).dt.total_seconds()
        # Filter to only include data after initial time
        df = df[df['elapsed_seconds'] >= 0].copy()
        if df.empty:
            print(f"  ⚠️  No data after initial time")
            return False
        time_col = 'elapsed_seconds'
    else:
        # Fallback to absolute time
        time_col = 'datetime_parsed'
    
    # Get metric name from filename
    metric_name = csv_path.stem
    
    # Get unit for this metric from dashboard
    unit_code = units_map.get(metric_name, '')
    
    # Determine if we need to convert values and what the label should be
    conversion_factor = 1.0
    ylabel = get_unit_label(unit_code) if unit_code else 'Value'
    
    # Check if this is a memory metric (needs conversion from bytes to MB)
    if 'Memory' in metric_name and unit_code in ['bytes', 'decbytes']:
        conversion_factor = 1.0 / (1024 * 1024)  # Bytes to MB
        ylabel = 'MB'
    
    # Check if this is a disk throughput metric (needs conversion from bytes/s to KiB/s)
    elif 'Disk' in metric_name and unit_code == 'binBps':
        conversion_factor = 1.0 / 1024  # Bytes/s to KiB/s
        ylabel = 'KiB/s'
    
    # Check if this is a network throughput metric (needs conversion from bytes/s to KiB/s)
    elif 'Network' in metric_name and unit_code == 'binBps':
        conversion_factor = 1.0 / 1024  # Bytes/s to KiB/s
        ylabel = 'KiB/s'
    
    # Identify data columns (exclude timestamp, datetime, datetime_parsed, elapsed_seconds, and any _min versions)
    exclude_cols = {'timestamp', 'datetime', 'datetime_parsed', 'elapsed_seconds', 'elapsed_seconds_min'}
    # Also exclude any columns that end with '_min' (time display columns)
    data_cols = [col for col in df.columns if col not in exclude_cols and not col.endswith('_min')]
    
    if not data_cols:
        print(f"  ❌ No data columns found")
        return False
    
    # Apply conversion to data columns if needed
    if conversion_factor != 1.0:
        for col in data_cols:
            df[col] = df[col] * conversion_factor
    
    # Create figure
    plt.figure(figsize=(12, 6))
    
    # Determine if we need to convert to minutes for display
    use_minutes = False
    if initial_time is not None:
        time_range = df[time_col].max() - df[time_col].min()
        if time_range > 600:  # More than 10 minutes
            use_minutes = True
            df[time_col + '_min'] = df[time_col] / 60
            time_col_display = time_col + '_min'
        else:
            time_col_display = time_col
    else:
        time_col_display = time_col
    
    # Plot each series
    for col in data_cols:
        # Clean up column name for legend
        if col == 'value':
            label = metric_name
        else:
            # Extract meaningful parts from label strings like "instance=192.168.0.170:9462, name=caddy"
            # Try to extract name= value
            if 'name=' in col:
                parts = col.split('name=')
                if len(parts) > 1:
                    label = parts[1].split(',')[0].strip()
                else:
                    label = col
            else:
                label = col
        
        # Plot the series
        plt.plot(df[time_col_display], df[col], label=label, linewidth=1.5, alpha=0.9)
    
    # Styling
    plt.title(metric_name, fontsize=16, fontweight='bold', pad=20)
    plt.ylabel(ylabel, fontsize=12, fontweight='bold')
    
    # Format x-axis
    ax = plt.gca()
    
    if initial_time is not None:
        # Use elapsed time
        time_range_seconds = df[time_col].max() - df[time_col].min()
        
        # Determine if we should use seconds or minutes
        if not use_minutes:
            plt.xlabel('Elapsed time (s)', fontsize=12, fontweight='bold')
            # Show every 30 seconds for short experiments
            if time_range_seconds <= 300:
                ax.xaxis.set_major_locator(plt.MultipleLocator(30))
            else:
                ax.xaxis.set_major_locator(plt.MultipleLocator(60))
            
            # Add VU level markers (in seconds) - vertical lines only
            if vu_levels:
                for level in vu_levels:
                    level_time = level['time']
                    if df[time_col_display].min() <= level_time <= df[time_col_display].max():
                        # Vertical line at time division
                        ax.axvline(x=level_time, color='gray', linestyle='--', alpha=0.5, linewidth=1)
                        # Add label at top
                        vu_level = level['vu_level']
                        y_max = ax.get_ylim()[1]
                        ax.text(level_time, y_max * 0.98, f'VU={vu_level}', 
                               rotation=90, verticalalignment='top', horizontalalignment='right',
                               fontsize=8, alpha=0.7, bbox=dict(boxstyle='round,pad=0.3', 
                               facecolor='white', alpha=0.7, edgecolor='gray', linewidth=0.5))
        else:
            plt.xlabel('Elapsed time (min)', fontsize=12, fontweight='bold')
            time_range_minutes = time_range_seconds / 60
            # Show every 2 minutes
            ax.xaxis.set_major_locator(plt.MultipleLocator(2))
            
            # Add VU level markers (in minutes) - vertical lines only
            if vu_levels:
                for level in vu_levels:
                    level_time = level['time'] / 60
                    if df[time_col_display].min() <= level_time <= df[time_col_display].max():
                        # Vertical line at time division
                        ax.axvline(x=level_time, color='gray', linestyle='--', alpha=0.5, linewidth=1)
                        # Add label at top
                        vu_level = level['vu_level']
                        y_max = ax.get_ylim()[1]
                        ax.text(level_time, y_max * 0.98, f'VU={vu_level}', 
                               rotation=90, verticalalignment='top', horizontalalignment='right',
                               fontsize=8, alpha=0.7, bbox=dict(boxstyle='round,pad=0.3', 
                               facecolor='white', alpha=0.7, edgecolor='gray', linewidth=0.5))
    else:
        # Fallback to absolute time formatting
        plt.xlabel('Time', fontsize=12, fontweight='bold')
        time_range = (df[time_col].max() - df[time_col].min()).total_seconds()
        
        if time_range <= 60:
            ax.xaxis.set_major_locator(mdates.SecondLocator(interval=10))
            ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        elif time_range <= 300:
            ax.xaxis.set_major_locator(mdates.SecondLocator(interval=30))
            ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        elif time_range <= 1800:
            ax.xaxis.set_major_locator(mdates.MinuteLocator(interval=2))
            ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        elif time_range <= 3600:
            ax.xaxis.set_major_locator(mdates.MinuteLocator(interval=5))
            ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M'))
        else:
            ax.xaxis.set_major_locator(mdates.MinuteLocator(interval=10))
            ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M'))
        plt.xticks(rotation=45)
    
    # Grid and Limits
    plt.grid(True, linestyle='--', alpha=0.7)
    plt.xlim(df[time_col_display].min(), df[time_col_display].max())
    
    # Legend
    if len(data_cols) > 1:
        # Multiple series: place legend outside
        plt.legend(bbox_to_anchor=(1.02, 1), loc='upper left', borderaxespad=0, title="Series")
    # Single series: no legend needed
    
    # Save
    safe_filename = sanitize_filename(metric_name)
    output_path = output_dir / f"{safe_filename}.png"
    plt.tight_layout()
    plt.savefig(output_path, dpi=300)
    print(f"  ✓ Saved: {output_path}")
    plt.close()
    
    return True


def generate_graphs(input_dir: str = DEFAULT_INPUT_DIR, output_dir: str = DEFAULT_OUTPUT_DIR, 
                   dashboard_path: str = DEFAULT_DASHBOARD):
    """Generate graphs from all CSV files in the input directory."""
    input_path = Path(input_dir)
    output_path = Path(output_dir)
    
    if not input_path.exists():
        print(f"Error: Input directory '{input_dir}' not found.")
        print("Run export_metrics.py first to generate CSV files.")
        return
    
    # Load dashboard units
    print(f"Loading unit mappings from dashboard...")
    units_map = load_dashboard_units(dashboard_path)
    if units_map:
        print(f"  Loaded units for {len(units_map)} metrics")
    print()
    
    # Create output directory if it doesn't exist
    output_path.mkdir(parents=True, exist_ok=True)
    
    # Find initial time from VU data
    print("Finding initial experiment time...")
    initial_time = find_initial_time(input_path)
    if initial_time is not None:
        print(f"  Initial time: {initial_time}")
        vu_levels = identify_vu_levels(input_path, initial_time)
        print(f"  Found {len(vu_levels)} VU level boundaries")
    else:
        print("  ⚠️  Could not find initial time, using absolute time")
        initial_time = None
        vu_levels = []
    print()
    
    # Find all CSV files
    csv_files = list(input_path.glob('*.csv'))
    
    if not csv_files:
        print(f"No CSV files found in '{input_dir}'")
        return
    
    print(f"Found {len(csv_files)} CSV files in '{input_dir}'")
    print(f"Output directory: '{output_dir}'")
    print()
    
    successful = 0
    failed = 0
    
    for csv_file in sorted(csv_files):
        if plot_csv_file(csv_file, output_path, units_map, initial_time, vu_levels):
            successful += 1
        else:
            failed += 1
    
    print()
    print(f"{'='*60}")
    print(f"Plotting complete!")
    print(f"  Successful: {successful}")
    print(f"  Failed: {failed}")
    print(f"  Output directory: {output_path.absolute()}")
    print(f"{'='*60}")


def main():
    parser = argparse.ArgumentParser(
        description='Plot metrics from exported CSV files',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python plot_exported_metrics.py
  python plot_exported_metrics.py --input-dir my_metrics --output-dir my_graphs
        """
    )
    parser.add_argument('--input-dir', default=DEFAULT_INPUT_DIR,
                        help=f'Input directory containing CSV files (default: {DEFAULT_INPUT_DIR})')
    parser.add_argument('--output-dir', default=DEFAULT_OUTPUT_DIR,
                        help=f'Output directory for PNG files (default: {DEFAULT_OUTPUT_DIR})')
    parser.add_argument('--dashboard', default=DEFAULT_DASHBOARD,
                        help=f'Path to Grafana dashboard JSON (default: {DEFAULT_DASHBOARD})')
    
    args = parser.parse_args()
    
    generate_graphs(args.input_dir, args.output_dir, args.dashboard)


if __name__ == "__main__":
    main()

