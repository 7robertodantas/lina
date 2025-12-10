import subprocess
import json
import csv
import re
import os
import time
from datetime import datetime

# --- CONFIGURATION ---
OUTPUT_FILE = 'docker_stats.csv'

def parse_size(size_str):
    """Parses human-readable sizes (e.g., '10.5MiB', '2.3kB') to Megabytes (MB)."""
    size_str = size_str.strip()
    match = re.match(r"([0-9\.]+)([a-zA-Z]+)", size_str)
    if not match: return 0.0
    value, unit = match.groups()
    value = float(value)
    
    unit = unit.lower()
    if 'k' in unit: return value / 1024
    if 'm' in unit: return value
    if 'g' in unit: return value * 1024
    if 't' in unit: return value * 1024 * 1024
    if 'b' in unit and len(unit) == 1: return value / (1024*1024)
    return value

def parse_pair(pair_str):
    """Splits 'Input / Output' strings."""
    try:
        parts = pair_str.split(' / ')
        if len(parts) == 2:
            return parse_size(parts[0]), parse_size(parts[1])
    except:
        pass
    return 0.0, 0.0

def monitor_stream():
    print(f"Starting STREAMING monitoring... saving to {OUTPUT_FILE}")
    
    # Check if DOCKER_HOST is set to ensure we are monitoring the right machine
    docker_host = os.environ.get('DOCKER_HOST')
    if docker_host:
        print(f"Target: {docker_host}")
    else:
        print("Target: Localhost (DOCKER_HOST not set)")
        
    print("Press Ctrl+C to stop.")

    cmd = ['docker', 'stats', '--format', '{{json .}}']
    
    # Open the process
    process = subprocess.Popen(
        cmd, 
        stdout=subprocess.PIPE, 
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1 # Line buffered
    )

    previous_stats = {}

    with open(OUTPUT_FILE, 'w', newline='') as csvfile:
        fieldnames = [
            'timestamp', 'container', 
            'cpu_percent', 'memory_mb', 
            'net_rx_mb_s', 'net_tx_mb_s',
            'disk_read_mb_s', 'disk_write_mb_s'
        ]
        writer = csv.DictWriter(csvfile, fieldnames=fieldnames)
        writer.writeheader()
        csvfile.flush()

        try:
            for line in process.stdout:
                if not line.strip(): continue

                try:
                    # Clean up ANSI codes if any
                    clean_line = re.sub(r'\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])', '', line)
                    c = json.loads(clean_line)
                except json.JSONDecodeError:
                    continue

                current_sys_time = time.time()
                timestamp_str = datetime.now().strftime('%H:%M:%S')
                
                name = c.get('Name', 'unknown')

                # 1. Parse Cumulative Values
                net_rx_cum, net_tx_cum = parse_pair(c.get('NetIO', '0B / 0B'))
                blk_read_cum, blk_write_cum = parse_pair(c.get('BlockIO', '0B / 0B'))
                
                # 2. Calculate Throughput (Delta)
                net_rx_rate = 0.0
                net_tx_rate = 0.0
                disk_read_rate = 0.0
                disk_write_rate = 0.0
                
                if name in previous_stats:
                    prev = previous_stats[name]
                    time_delta = current_sys_time - prev['time']
                    
                    if time_delta > 0.1: 
                        net_rx_rate = (net_rx_cum - prev['net_rx']) / time_delta
                        net_tx_rate = (net_tx_cum - prev['net_tx']) / time_delta
                        disk_read_rate = (blk_read_cum - prev['blk_read']) / time_delta
                        disk_write_rate = (blk_write_cum - prev['blk_write']) / time_delta
                        
                        if net_rx_rate < 0: net_rx_rate = 0

                # 3. Update Previous Stats
                previous_stats[name] = {
                    'time': current_sys_time,
                    'net_rx': net_rx_cum, 'net_tx': net_tx_cum,
                    'blk_read': blk_read_cum, 'blk_write': blk_write_cum
                }

                # 4. Write & Flush
                writer.writerow({
                    'timestamp': timestamp_str,
                    'container': name,
                    'cpu_percent': float(c.get('CPUPerc', '0%').replace('%', '')),
                    'memory_mb': parse_size(c.get('MemUsage', '0B').split(' / ')[0]),
                    'net_rx_mb_s': round(net_rx_rate, 4),
                    'net_tx_mb_s': round(net_tx_rate, 4),
                    'disk_read_mb_s': round(disk_read_rate, 4),
                    'disk_write_mb_s': round(disk_write_rate, 4)
                })
                csvfile.flush()

        except KeyboardInterrupt:
            print("\nStopping...")
            process.terminate()
            process.wait()
            print("Done.")

if __name__ == "__main__":
    monitor_stream()