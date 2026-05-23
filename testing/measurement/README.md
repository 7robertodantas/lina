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

## Exporting Metrics from Grafana/Prometheus

The `export_metrics.py` script allows you to export all metrics from your Grafana monitoring dashboard for a specific time period to JSON and CSV files.

### Usage

1. **Export metrics for a time period**:
   ```bash
   python3 export_metrics.py \
     --from "2025-12-18T18:25:04.648Z" \
     --to "2025-12-18T18:34:57.053Z"
   ```

2. **Specify Prometheus URL** (if not running on localhost:9090):
   ```bash
   python3 export_metrics.py \
     --from "2025-12-18T18:25:04.648Z" \
     --to "2025-12-18T18:34:57.053Z" \
     --prometheus-url http://192.168.0.170:9090
   ```

   If the dashboard contains Grafana variables, the exporter uses the current
   values saved in the dashboard JSON. Override them explicitly when exporting a
   different target:
   ```bash
   python3 export_metrics.py \
     --from "2025-12-18T18:25:04.648Z" \
     --to "2025-12-18T18:34:57.053Z" \
     --prometheus-url http://localhost:9090 \
     --var instance=192.168.0.200:9463
   ```

3. **Export only JSON or CSV**:
   ```bash
   python3 export_metrics.py \
     --from "2025-12-18T18:25:04.648Z" \
     --to "2025-12-18T18:34:57.053Z" \
     --format csv
   ```

4. **Custom output directory**:
   ```bash
   python3 export_metrics.py \
     --from "2025-12-18T18:25:04.648Z" \
     --to "2025-12-18T18:34:57.053Z" \
     --output-dir my_metrics
   ```

### Output Format

The script extracts all Prometheus queries from the Grafana dashboard and exports each metric to separate files:

- **JSON files**: Contain the full Prometheus API response including metadata
- **CSV files**: Time-series data with timestamps, export-relative elapsed seconds, and values for each series
- **manifest.json**: Records the time range, substitutions, exported query list, and generated filenames

Files are named based on the panel title from the dashboard (sanitized for filesystem compatibility).
The exporter also requests k6 load-test marker metrics when they are available:
`k6_loadtest_stage_index`, `k6_loadtest_phase_code`,
`k6_loadtest_level_vus`, and `k6_loadtest_measurement_active`.

### Example Output

```
metrics_export/
├── Disk_write_throughput.json
├── Disk_write_throughput.csv
├── Container_CPU_Usage.json
├── Container_CPU_Usage.csv
├── Consumption_Recorded_per_second.json
├── Consumption_Recorded_per_second.csv
...
```

### Plotting Exported Metrics

After exporting metrics, you can generate graphs from the CSV files using `plot_exported_metrics.py`:

```bash
python3 plot_exported_metrics.py
```

This will:
- Read all CSV files from the `metrics_export/` directory
- Generate report-ready matplotlib graphs using elapsed time on the x-axis
- Shade warmup, measurement, and teardown periods directly in each graph
- Label measurement windows with the active device count
- Overlay the expected incoming report rate on throughput graphs
- Add default saturation threshold lines for 90% resource use and 5s latency
- Save PNG files to the `graphs/` directory

**Options:**
- `--input-dir`: Specify custom input directory (default: `metrics_export`)
- `--output-dir`: Specify custom output directory (default: `graphs`)
- `--level-vus`: Devices added per level when marker metrics are unavailable (default: `25`)
- `--level-count`: Number of load levels when marker metrics are unavailable (default: `8`)
- `--warmup`: Warmup duration per level when marker metrics are unavailable (default: `60s`)
- `--measure`: Measurement duration per level when marker metrics are unavailable (default: `120s`)
- `--report-interval-seconds`: Expected per-device usage report interval for incoming-rate overlays (default: `1`)

**Example:**
```bash
python3 plot_exported_metrics.py --input-dir my_metrics --output-dir my_graphs
```

The script automatically handles:
- Single-series metrics (one value column)
- Multi-series metrics (multiple labeled series)
- Proper datetime formatting on x-axis
- Clean legend placement
- **Automatic unit extraction from Grafana dashboard** - Y-axis labels use the correct units (e.g., "debits/s", "Bytes/s", "%", "s", "events/s", etc.)

**Note:** The script reads unit information from the Grafana dashboard JSON file. If a metric doesn't have a unit defined in the dashboard, it will use "Value" as the default label.

## Notes

- The CSV file is appended to, so you can run multiple measurement sessions
- Graphs are regenerated from the entire CSV file each time `plot_graphs.py` runs
- Make sure you have write permissions in the measurement directory
- For long-running measurement sessions, consider rotating the CSV file periodically
- The `export_metrics.py` script requires Prometheus to be accessible via HTTP API
