# Measurement Tools

This directory contains lightweight measurement tools that connect to a remote Docker daemon via SSH to collect container statistics and generate visualizations.

## Overview

The measurement system uses `docker stats` to collect real-time metrics from Docker containers without installing any monitoring agents on the target machine. It connects to the remote Docker daemon through SSH, making it ideal for measuring performance on remote deployments.

## Features

- **Zero overhead on target machine**: Only reads existing Docker stats, doesn't install agents
- **SSH-based monitoring**: Connect to remote Docker daemons via SSH
- **Real-time streaming**: Collects metrics continuously and saves to CSV
- **Automatic visualization**: Generates graphs for CPU, memory, network, and disk I/O

## Prerequisites

- Python 3.9+
- Docker CLI (for connecting to remote Docker daemon)
- SSH access to target machine (if monitoring remotely)

## Setup

1. **Create virtual environment** (recommended):
   ```bash
   cd measurement
   python3 -m venv venv
   source venv/bin/activate  # On Windows: venv\Scripts\activate
   ```

2. **Install dependencies**:
   ```bash
   pip install -r requirements.txt
   ```

## Usage

### Measuring Remote Docker Host

1. **Set DOCKER_HOST environment variable** to connect via SSH:
   ```bash
   export DOCKER_HOST="ssh://user@hostname"
   # Or with custom SSH port:
   export DOCKER_HOST="ssh://user@hostname:2222"
   ```

2. **Start measurement**:
   ```bash
   python3 monitor.py
   ```
   
   The script will:
   - Stream `docker stats` data
   - Calculate throughput rates (network and disk I/O)
   - Save metrics to `docker_stats.csv`
   - Press `Ctrl+C` to stop

3. **Generate graphs**:
   ```bash
   python3 plot_graphs.py
   ```
   
   This creates visualization graphs in the `graphs/` directory:
   - `01_cpu_usage.png` - CPU usage over time
   - `02_memory_usage.png` - Memory usage over time
   - `03_network_rx.png` - Network receive rate
   - `04_network_tx.png` - Network transmit rate
   - `05_disk_read.png` - Disk read rate
   - `06_disk_write.png` - Disk write rate

### Measuring Local Docker

If `DOCKER_HOST` is not set, the script will measure the local Docker daemon:

```bash
python3 monitor.py
```

## Output Files

- `docker_stats.csv` - Raw metrics data with columns:
  - `timestamp` - Time of measurement (HH:MM:SS)
  - `container` - Container name
  - `cpu_percent` - CPU usage percentage
  - `memory_mb` - Memory usage in MB
  - `net_rx_mb_s` - Network receive throughput (MB/s)
  - `net_tx_mb_s` - Network transmit throughput (MB/s)
  - `disk_read_mb_s` - Disk read throughput (MB/s)
  - `disk_write_mb_s` - Disk write throughput (MB/s)

- `graphs/` - Directory containing generated visualization PNG files

## How It Works

1. **monitor.py**: 
   - Runs `docker stats --format '{{json .}}'` continuously
   - Parses JSON output and calculates rate deltas
   - Writes metrics to CSV file in real-time

2. **plot_graphs.py**:
   - Reads the CSV file
   - Generates time-series plots for each metric
   - Saves high-resolution PNG images (300 DPI)

## Resource Usage

- **Target machine**: Minimal overhead - only reads existing Docker daemon metrics
- **Host machine**: Very low overhead - lightweight Python script for parsing and CSV writing

The measurement approach is non-intrusive and doesn't generate additional load on the target system.

## Notes

- The CSV file is appended to, so you can run multiple measurement sessions
- Graphs are regenerated from the entire CSV file each time `plot_graphs.py` runs
- Make sure you have write permissions in the measurement directory
- For long-running measurement sessions, consider rotating the CSV file periodically
