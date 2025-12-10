# Monitoring Scripts

This directory contains scripts for collecting and analyzing system performance measurements.

## Scripts

### `start-measurement.sh`
Starts collecting system performance metrics:
- Docker container statistics (CPU, memory, network, I/O)
- CPU statistics (mpstat)
- Process statistics (pidstat)
- Virtual memory statistics (vmstat)
- I/O statistics (iostat)

**Usage:**
```bash
./monitoring/start-measurement.sh
```

The script will run in the background and collect data for the configured duration (default: 5 minutes).

### `stop-measurement.sh`
Stops all running measurement processes.

**Usage:**
```bash
./monitoring/stop-measurement.sh
```

### `copy-measurements.sh`
Copies measurement log files from a remote environment to the local `./measurements/` directory.

**Usage:**
```bash
./monitoring/copy-measurements.sh user@hostname
./monitoring/copy-measurements.sh user@hostname -p 2222
```

### `convert-measurements.sh`
Converts raw log files to CSV format for easier analysis.

**Usage:**
```bash
./monitoring/convert-measurements.sh [measurements_directory]
# Default: ./measurements
```

### `plot-measurements.py`
Creates visualizations from CSV measurement data.

**Usage:**
```bash
python3 monitoring/plot-measurements.py [measurements_directory]
# Default: ./measurements
```

**Requirements:**
```bash
pip install pandas matplotlib
```

## Output Files

All measurement data is saved in CSV format:
- `docker_stats.csv` - Docker container statistics
- `mpstat.csv` - CPU statistics
- `pidstat.csv` - Process statistics
- `vmstat.csv` - Virtual memory statistics
- `iostat.csv` - I/O statistics
- `measurement_stdout.log` - Standard output from measurement processes
- `measurement_stderr.log` - Standard error (warnings, errors)

## Workflow

1. **Start measurements on remote machine:**
   ```bash
   ssh user@hostname "cd ~/lnpay && ./start-measurement.sh"
   ```

2. **Wait for measurements to complete, then copy:**
   ```bash
   ./monitoring/copy-measurements.sh user@hostname
   ```

3. **Convert any remaining logs to CSV (if needed):**
   ```bash
   ./monitoring/convert-measurements.sh
   ```

4. **Plot the data:**
   ```bash
   python3 monitoring/plot-measurements.py
   ```

## Configuration

Edit `start-measurement.sh` to adjust:
- `DURATION_MINUTES` - How long to collect data (default: 5 minutes)
- `INTERVAL` - Seconds between each measurement (default: 5 seconds)

## Notes

- Measurement data is stored in `./measurements/` (gitignored)
- All scripts are copied to remote machines during deployment via `scripts/deploy.sh`
- CSV files can be analyzed with Python, R, Excel, or any data analysis tool

