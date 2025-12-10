import pandas as pd
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
import seaborn as sns
import sys
import os

# --- CONFIGURATION ---
INPUT_FILE = 'docker_stats.csv'
OUTPUT_DIR = 'graphs'  # Folder to save images
sns.set_theme(style="whitegrid") # Academic/Clean style

def generate_graphs():
    if not os.path.exists(INPUT_FILE):
        print(f"Error: {INPUT_FILE} not found.")
        return

    # Create output directory if it doesn't exist
    if not os.path.exists(OUTPUT_DIR):
        os.makedirs(OUTPUT_DIR)

    print(f"Loading data from {INPUT_FILE}...")
    try:
        df = pd.read_csv(INPUT_FILE)
    except pd.errors.EmptyDataError:
        print("CSV is empty. Run the monitor first.")
        return

    # Convert timestamp column to datetime objects
    # We attach the current date to the time so matplotlib can calculate spacing
    df['timestamp'] = pd.to_datetime(df['timestamp'], format='%H:%M:%S')

    # Get unique containers
    containers = df['container'].unique()
    print(f"Found containers: {', '.join(containers)}")

    # Helper function to plot one metric
    def save_plot(metric_col, title, ylabel, filename):
        plt.figure(figsize=(12, 6))
        
        for container in containers:
            subset = df[df['container'] == container]
            
            # Optional: Add .rolling(3).mean() if data is too jittery
            plt.plot(subset['timestamp'], subset[metric_col], label=container, linewidth=1.5, alpha=0.9)
        
        # Styling
        plt.title(title, fontsize=16, fontweight='bold', pad=20)
        plt.ylabel(ylabel, fontsize=12, fontweight='bold')
        plt.xlabel('Time', fontsize=12, fontweight='bold')
        
        # X-Axis formatting (Hour:Minute:Second)
        ax = plt.gca()
        ax.xaxis.set_major_formatter(mdates.DateFormatter('%H:%M:%S'))
        plt.xticks(rotation=45)
        
        # Grid and Limits
        plt.grid(True, linestyle='--', alpha=0.7)
        plt.xlim(df['timestamp'].min(), df['timestamp'].max())
        
        # Move legend outside to prevent blocking data
        plt.legend(bbox_to_anchor=(1.02, 1), loc='upper left', borderaxespad=0, title="Containers")
        
        # Save
        output_path = os.path.join(OUTPUT_DIR, filename)
        plt.tight_layout()
        plt.savefig(output_path, dpi=300)
        print(f"Saved: {output_path}")
        plt.close()

    # --- GENERATE PLOTS ---
    
    # 1. CPU
    save_plot('cpu_percent', 'CPU Usage Over Time', 'CPU (%)', '01_cpu_usage.png')
    
    # 2. Memory
    save_plot('memory_mb', 'Memory Usage Over Time', 'Memory (MB)', '02_memory_usage.png')
    
    # 3. Network Receive (Download)
    save_plot('net_rx_mb_s', 'Network Receive Rate', 'Throughput (MB/s)', '03_network_rx.png')
    
    # 4. Network Transmit (Upload)
    save_plot('net_tx_mb_s', 'Network Transmit Rate', 'Throughput (MB/s)', '04_network_tx.png')
    
    # 5. Disk Read
    save_plot('disk_read_mb_s', 'Disk Read Rate', 'Throughput (MB/s)', '05_disk_read.png')
    
    # 6. Disk Write
    save_plot('disk_write_mb_s', 'Disk Write Rate', 'Throughput (MB/s)', '06_disk_write.png')

    print("\nAll graphs generated successfully in the 'graphs' folder.")

if __name__ == "__main__":
    generate_graphs()