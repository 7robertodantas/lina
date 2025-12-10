import pandas as pd
import matplotlib.pyplot as plt
import matplotlib.ticker as ticker
import seaborn as sns
import sys
import os

# --- CONFIGURATION ---
INPUT_FILE = 'docker_stats.csv'
OUTPUT_DIR = 'graphs'
sns.set_theme(style="whitegrid")

def parse_time(t_str):
    """Parses MM:SS string into a timedelta or simple float minutes for plotting"""
    try:
        parts = t_str.split(':')
        minutes = int(parts[0])
        seconds = int(parts[1])
        return minutes * 60 + seconds
    except:
        return 0

def generate_total_graphs():
    if not os.path.exists(INPUT_FILE):
        print(f"Error: {INPUT_FILE} not found.")
        return

    if not os.path.exists(OUTPUT_DIR):
        os.makedirs(OUTPUT_DIR)

    print(f"Loading data from {INPUT_FILE}...")
    df = pd.read_csv(INPUT_FILE)

    # Convert 'timestamp' (MM:SS) to total seconds for proper sorting and x-axis
    df['seconds_elapsed'] = df['timestamp'].apply(parse_time)
    df = df.sort_values(['seconds_elapsed', 'container'])

    # Get unique containers and timestamps
    containers = df['container'].unique()
    timestamps = df['seconds_elapsed'].unique()

    # Pivot the data: Rows=Time, Cols=Containers, Values=Metric
    # This aligns all containers to the exact same timestamps for stacking
    def get_pivot(metric):
        pivot_df = df.pivot_table(index='seconds_elapsed', columns='container', values=metric, fill_value=0)
        return pivot_df

    # Helper to plot Stacked Area Chart
    def save_stacked_plot(metric_col, title, ylabel, filename):
        pivot_data = get_pivot(metric_col)
        
        fig, ax = plt.subplots(figsize=(12, 7))
        
        # Create the Stackplot
        ax.stackplot(pivot_data.index, pivot_data.T.values, labels=pivot_data.columns, alpha=0.85)
        
        # Styling
        ax.set_title(title, fontsize=16, fontweight='bold', pad=20)
        ax.set_ylabel(ylabel, fontsize=13, fontweight='bold')
        ax.set_xlabel('Time (Elapsed)', fontsize=13, fontweight='bold')
        ax.set_xlim(pivot_data.index.min(), pivot_data.index.max())
        
        # Format X-axis to show MM:SS instead of raw seconds
        def seconds_to_fmt(x, pos):
            m = int(x // 60)
            s = int(x % 60)
            return f"{m:02d}:{s:02d}"
        ax.xaxis.set_major_formatter(ticker.FuncFormatter(seconds_to_fmt))
        
        # Add a "Total" line on top (optional, but helps readability)
        total_series = pivot_data.sum(axis=1)
        ax.plot(pivot_data.index, total_series, color='black', linewidth=1.5, linestyle='--', alpha=0.5, label='_nolegend_')

        # Legend outside
        handles, labels = ax.get_legend_handles_labels()
        # Reverse legend order to match the visual stack order (usually helps intuitive reading)
        ax.legend(reversed(handles), reversed(labels), bbox_to_anchor=(1.02, 1), loc='upper left', title="Containers")
        
        plt.tight_layout()
        output_path = os.path.join(OUTPUT_DIR, filename)
        plt.savefig(output_path, dpi=300)
        print(f"Saved: {output_path}")
        plt.close()

    # --- GENERATE PLOTS ---
    print("Generating stacked area charts...")
    
    # 1. Total CPU
    save_stacked_plot('cpu_percent', 'Total Application CPU Load', 'Cumulative CPU (%)', 'total_cpu.png')
    
    # 2. Total Memory
    save_stacked_plot('memory_mb', 'Total Application Memory Footprint', 'Cumulative Memory (MB)', 'total_memory.png')
    
    # 3. Network Traffic (Combined Rx+Tx or just Rx)
    # Let's sum Rx and Tx for a "Total Bandwidth" view
    df['net_total_mb_s'] = df['net_rx_mb_s'] + df['net_tx_mb_s']
    save_stacked_plot('net_total_mb_s', 'Total Network Bandwidth Usage', 'Throughput (MB/s)', 'total_network.png')
    
    # 4. Disk I/O (Combined Read+Write)
    df['disk_total_mb_s'] = df['disk_read_mb_s'] + df['disk_write_mb_s']
    save_stacked_plot('disk_total_mb_s', 'Total Disk I/O Activity', 'Throughput (MB/s)', 'total_disk.png')

    print("\nDone. Check the 'graphs_total' folder.")

if __name__ == "__main__":
    generate_total_graphs()