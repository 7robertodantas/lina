#!/bin/bash
# Convert measurement log files to CSV format for analysis
# This script processes the raw log files and converts them to CSV
#
# Usage: ./convert-measurements.sh [measurements_directory]
# Default: ./measurements

set -e

MEASUREMENTS_DIR="${1:-./measurements}"

if [ ! -d "$MEASUREMENTS_DIR" ]; then
    echo "Error: Directory $MEASUREMENTS_DIR not found"
    echo "Usage: ./convert-measurements.sh [measurements_directory]"
    exit 1
fi

echo "Converting measurement logs to CSV format..."
echo "Directory: $MEASUREMENTS_DIR"
echo ""

# Convert vmstat.log to CSV (if exists)
if [ -f "$MEASUREMENTS_DIR/vmstat.log" ]; then
    echo "Converting vmstat.log to CSV..."
    # vmstat output: skip first 2 lines (header), convert spaces to commas
    awk 'NR>2 {gsub(/ +/, ","); print}' "$MEASUREMENTS_DIR/vmstat.log" > "$MEASUREMENTS_DIR/vmstat.csv"
    echo "  ✓ Created vmstat.csv"
fi

# docker_stats.csv should already be in CSV format, but ensure it has a header
if [ -f "$MEASUREMENTS_DIR/docker_stats.csv" ]; then
    # Check if header exists
    if ! head -1 "$MEASUREMENTS_DIR/docker_stats.csv" | grep -q "timestamp\|Name"; then
        echo "Adding header to docker_stats.csv..."
        # Add header if missing
        echo "timestamp,Name,CPUPerc,MemUsage,NetIO,BlockIO" > "$MEASUREMENTS_DIR/docker_stats.csv.tmp"
        cat "$MEASUREMENTS_DIR/docker_stats.csv" >> "$MEASUREMENTS_DIR/docker_stats.csv.tmp"
        mv "$MEASUREMENTS_DIR/docker_stats.csv.tmp" "$MEASUREMENTS_DIR/docker_stats.csv"
        echo "  ✓ Added header to docker_stats.csv"
    fi
fi

# Note: mpstat.csv, pidstat.csv, and iostat.csv should already be in CSV format
# (they're generated with -o CSV flag)

echo ""
echo "Conversion complete!"
echo ""
echo "CSV files available in: $MEASUREMENTS_DIR"
echo "  - docker_stats.csv (Docker container stats)"
echo "  - mpstat.csv (CPU statistics)"
echo "  - pidstat.csv (Process statistics)"
echo "  - vmstat.csv (Virtual memory statistics)"
echo "  - iostat.csv (I/O statistics)"
echo ""
echo "You can now use these CSV files with:"
echo "  - Python (pandas + matplotlib/plotly)"
echo "  - R"
echo "  - Excel/LibreOffice"
echo "  - Jupyter notebooks"
echo ""

