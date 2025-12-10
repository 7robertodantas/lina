#!/bin/bash
# Configuration:
#   DURATION_MINUTES: Total time to collect measurements (in minutes)
#   INTERVAL: Seconds between each measurement
#   SAMPLES: Automatically calculated = (DURATION_MINUTES * 60) / INTERVAL
#
# Examples for 5 minutes:
#   INTERVAL=5  → 60 samples (one every 5 seconds)
#   INTERVAL=10 → 30 samples (one every 10 seconds)
#   INTERVAL=1  → 300 samples (one every second)
DURATION_MINUTES=5
INTERVAL=5
SAMPLES=$(( (DURATION_MINUTES*60) / INTERVAL ))

echo "Running for $DURATION_MINUTES minutes ($SAMPLES samples)..."

# Docker stats
(
  for i in $(seq 1 $SAMPLES); do
    docker stats --no-stream \
      --format "{{.Name}},{{.CPUPerc}},{{.MemUsage}},{{.NetIO}},{{.BlockIO}}" \
      2>> measurement_stderr.log \
      | ts '%Y-%m-%d %H:%M:%S' 2>> measurement_stderr.log \
      >> docker_stats.csv
    sleep $INTERVAL
  done
) >> measurement_stdout.log 2>> measurement_stderr.log &
echo $! > docker_stats.pid

# mpstat (CSV format for easier analysis)
mpstat $INTERVAL $SAMPLES -o CSV > mpstat.csv 2>> measurement_stderr.log &
echo $! > mpstat.pid

# pidstat (CSV format)
pidstat -u -r $INTERVAL $SAMPLES -o CSV > pidstat.csv 2>> measurement_stderr.log &
echo $! > pidstat.pid

# vmstat (space-separated, will convert to CSV)
vmstat $INTERVAL $SAMPLES > vmstat.log 2>> measurement_stderr.log &
echo $! > vmstat.pid

# iostat (CSV format)
iostat -xz $INTERVAL $SAMPLES -o CSV > iostat.csv 2>> measurement_stderr.log &
echo $! > iostat.pid

echo "All monitors started."
