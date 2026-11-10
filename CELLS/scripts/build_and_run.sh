#!/bin/bash

# Build the CELLS project
echo "Building CELLS project..."
go build -o CELLS cmd/CELLS/main.go
if [ $? -ne 0 ]; then
  echo "Build failed."
  exit 1
fi
echo "Build succeeded."

# Run the CELLS node
echo "Starting CELLS node..."
./CELLS

# Note: Ensure environment variables are set as needed, e.g. USE_SCYLLA=true for ScyllaDB usage
