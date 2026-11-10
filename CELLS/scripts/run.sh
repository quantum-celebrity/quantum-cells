#!/bin/bash
# Run script for CELLS on Linux
# Sets up environment and runs CELLS

set -e

echo "=== Starting CELLS ==="
echo ""

# Check if binary exists
if [ ! -f "./CELLS" ]; then
    echo "ERROR: CELLS binary not found!"
    echo "Please build first:"
    echo "  ./build.sh"
    exit 1
fi

# Set library path for liboqs
LIBOQS_PATHS=(
    "$(pwd)/liboqs_install/lib64"
    "$(pwd)/liboqs_install/lib"
    "/usr/local/lib64"
    "/usr/local/lib"
    "/opt/liboqs/lib64"
    "/opt/liboqs/lib"
)

for path in "${LIBOQS_PATHS[@]}"; do
    if [ -d "$path" ] && [ -f "$path/liboqs.so" ]; then
        export LD_LIBRARY_PATH="$path:${LD_LIBRARY_PATH:-}"
        echo "Found liboqs at: $path"
        break
    fi
done

# Load environment variables from .env if it exists
if [ -f ".env" ]; then
    echo "Loading environment variables from .env..."
    export $(grep -v '^#' .env | xargs)
fi

# Run CELLS
echo ""
echo "Starting CELLS node..."
echo ""

./CELLS

