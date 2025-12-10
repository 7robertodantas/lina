#!/usr/bin/env python3
"""
Plot measurement data from CSV files
Creates visualizations for system performance metrics

Usage:
    python3 plot-measurements.py [measurements_directory]
    Default: ./measurements

Requirements:
    pip install pandas matplotlib
"""

import sys
import os
import pandas as pd
import matplotlib.pyplot as plt
from pathlib import Path

def load_csv(filepath):
    """Load CSV file, handling different formats"""
    try:
        # Try reading with default settings
        df = pd.read_csv(filepath)
        return df
    except Exception as e:
        print(f"Warning: Could not read {filepath}: {e}")
        return None

def plot_docker_stats(df, output_dir):
    """Plot Docker container statistics"""
    if df is None or df.empty:
        return
    
    # Parse timestamp if present
    if 'timestamp' in df.columns or df.columns[0].lower() == 'timestamp':
        try:
            df['timestamp'] = pd.to_datetime(df.iloc[:, 0])
        except:
            pass
    
    fig, axes = plt.subplots(2, 2, figsize=(15, 10))
    fig.suptitle('Docker Container Statistics', fontsize=16)
    
    # CPU Percentage
    if 'CPUPerc' in df.columns:
        ax = axes[0, 0]
        for col in df.columns:
            if 'CPUPerc' in col or 'CPU' in col:
                ax.plot(df.index, pd.to_numeric(df[col].str.rstrip('%'), errors='coerce'), label=col)
        ax.set_title('CPU Usage (%)')
        ax.set_xlabel('Sample')
        ax.set_ylabel('CPU %')
        ax.legend()
        ax.grid(True)
    
    # Memory Usage
    if 'MemUsage' in df.columns:
        ax = axes[0, 1]
        for col in df.columns:
            if 'MemUsage' in col or 'Mem' in col:
                # Try to parse memory (e.g., "1.5GiB / 2GiB")
                mem_data = df[col].str.split(' / ').str[0].str.rstrip('GiB').str.rstrip('MiB')
                ax.plot(df.index, pd.to_numeric(mem_data, errors='coerce'), label=col)
        ax.set_title('Memory Usage')
        ax.set_xlabel('Sample')
        ax.set_ylabel('Memory')
        ax.legend()
        ax.grid(True)
    
    # Network I/O
    if 'NetIO' in df.columns:
        ax = axes[1, 0]
        for col in df.columns:
            if 'NetIO' in col or 'Net' in col:
                ax.plot(df.index, df[col], label=col)
        ax.set_title('Network I/O')
        ax.set_xlabel('Sample')
        ax.set_ylabel('Network')
        ax.legend()
        ax.grid(True)
    
    # Block I/O
    if 'BlockIO' in df.columns:
        ax = axes[1, 1]
        for col in df.columns:
            if 'BlockIO' in col or 'Block' in col:
                ax.plot(df.index, df[col], label=col)
        ax.set_title('Block I/O')
        ax.set_xlabel('Sample')
        ax.set_ylabel('Block I/O')
        ax.legend()
        ax.grid(True)
    
    plt.tight_layout()
    plt.savefig(os.path.join(output_dir, 'docker_stats.png'), dpi=150)
    print("  ✓ Created docker_stats.png")

def plot_mpstat(df, output_dir):
    """Plot CPU statistics from mpstat"""
    if df is None or df.empty:
        return
    
    # mpstat CSV format: timestamp,CPU,%usr,%nice,%sys,%iowait,%irq,%soft,%steal,%guest,%gnice,%idle
    fig, axes = plt.subplots(2, 2, figsize=(15, 10))
    fig.suptitle('CPU Statistics (mpstat)', fontsize=16)
    
    numeric_cols = df.select_dtypes(include=['float64', 'int64']).columns
    
    if '%usr' in df.columns:
        axes[0, 0].plot(df.index, df['%usr'], label='User', color='blue')
        axes[0, 0].plot(df.index, df['%sys'], label='System', color='red')
        axes[0, 0].set_title('CPU Usage by Type')
        axes[0, 0].set_xlabel('Sample')
        axes[0, 0].set_ylabel('CPU %')
        axes[0, 0].legend()
        axes[0, 0].grid(True)
    
    if '%iowait' in df.columns:
        axes[0, 1].plot(df.index, df['%iowait'], label='I/O Wait', color='orange')
        axes[0, 1].set_title('I/O Wait')
        axes[0, 1].set_xlabel('Sample')
        axes[0, 1].set_ylabel('CPU %')
        axes[0, 1].legend()
        axes[0, 1].grid(True)
    
    if '%idle' in df.columns:
        axes[1, 0].plot(df.index, df['%idle'], label='Idle', color='green')
        axes[1, 0].set_title('CPU Idle Time')
        axes[1, 0].set_xlabel('Sample')
        axes[1, 0].set_ylabel('CPU %')
        axes[1, 0].legend()
        axes[1, 0].grid(True)
    
    # Overall CPU usage (100 - idle)
    if '%idle' in df.columns:
        cpu_usage = 100 - df['%idle']
        axes[1, 1].plot(df.index, cpu_usage, label='Total CPU Usage', color='purple')
        axes[1, 1].set_title('Total CPU Usage')
        axes[1, 1].set_xlabel('Sample')
        axes[1, 1].set_ylabel('CPU %')
        axes[1, 1].legend()
        axes[1, 1].grid(True)
    
    plt.tight_layout()
    plt.savefig(os.path.join(output_dir, 'mpstat.png'), dpi=150)
    print("  ✓ Created mpstat.png")

def plot_vmstat(df, output_dir):
    """Plot virtual memory statistics"""
    if df is None or df.empty:
        return
    
    fig, axes = plt.subplots(2, 2, figsize=(15, 10))
    fig.suptitle('Virtual Memory Statistics (vmstat)', fontsize=16)
    
    # Common vmstat columns (adjust based on your actual columns)
    if len(df.columns) >= 4:
        # Memory columns (typically: r, b, swpd, free, buff, cache)
        if 'free' in df.columns.str.lower().str[0] if len(df.columns) > 3 else False:
            col_idx = [i for i, c in enumerate(df.columns) if 'free' in str(c).lower()][0] if any('free' in str(c).lower() for c in df.columns) else 3
            axes[0, 0].plot(df.index, pd.to_numeric(df.iloc[:, col_idx], errors='coerce'))
            axes[0, 0].set_title('Free Memory')
            axes[0, 0].set_xlabel('Sample')
            axes[0, 0].set_ylabel('Memory (KB)')
            axes[0, 0].grid(True)
    
    plt.tight_layout()
    plt.savefig(os.path.join(output_dir, 'vmstat.png'), dpi=150)
    print("  ✓ Created vmstat.png")

def main():
    measurements_dir = sys.argv[1] if len(sys.argv) > 1 else "./measurements"
    
    if not os.path.isdir(measurements_dir):
        print(f"Error: Directory {measurements_dir} not found")
        print("Usage: python3 plot-measurements.py [measurements_directory]")
        sys.exit(1)
    
    print("Plotting measurement data...")
    print(f"Directory: {measurements_dir}")
    print("")
    
    # Plot Docker stats
    docker_file = os.path.join(measurements_dir, "docker_stats.csv")
    if os.path.exists(docker_file):
        print("Processing docker_stats.csv...")
        df = load_csv(docker_file)
        plot_docker_stats(df, measurements_dir)
    
    # Plot mpstat
    mpstat_file = os.path.join(measurements_dir, "mpstat.csv")
    if os.path.exists(mpstat_file):
        print("Processing mpstat.csv...")
        df = load_csv(mpstat_file)
        plot_mpstat(df, measurements_dir)
    
    # Plot vmstat
    vmstat_file = os.path.join(measurements_dir, "vmstat.csv")
    if os.path.exists(vmstat_file):
        print("Processing vmstat.csv...")
        df = load_csv(vmstat_file)
        plot_vmstat(df, measurements_dir)
    
    print("")
    print("Plotting complete!")
    print(f"Charts saved in: {measurements_dir}")
    print("")
    print("Note: Install requirements with:")
    print("  pip install pandas matplotlib")

if __name__ == "__main__":
    main()

