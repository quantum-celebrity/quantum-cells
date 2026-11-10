#!/bin/bash
# CELLS Migration Setup Script
# Run this script on the target server to set up the project

set -e

echo "=== CELLS Migration Setup ==="
echo ""

# Check prerequisites
echo "Checking prerequisites..."
command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "Error: npm is not installed"; exit 1; }
command -v sqlite3 >/dev/null 2>&1 || { echo "Warning: sqlite3 not found - database import may fail"; }
echo "Prerequisites OK"
echo ""

# Install Go dependencies
echo "Installing Go dependencies..."
cd source
go mod download
go mod verify
cd ..
echo "Go dependencies installed"
echo ""

# Install Node.js dependencies
echo "Installing Node.js dependencies..."
npm install
echo "Node.js dependencies installed"
echo ""

# Import databases
echo "Importing databases..."
if [ -f "databases/CELLS_*.sql" ]; then
    DB_FILE=ls databases/CELLS_*.sql | head -1
    echo "Importing main database from $DB_FILE..."
    sqlite3 databases/CELLS.db < $DB_FILE
    chmod 644 databases/CELLS.db
fi

if [ -f "databases/transactions_*.sql" ]; then
    TX_FILE=ls databases/transactions_*.sql | head -1
    echo "Importing transactions database from $TX_FILE..."
    sqlite3 databases/transactions.db < $TX_FILE
    chmod 644 databases/transactions.db
fi

# Or copy binary databases if SQL dumps don't exist
if [ ! -f "databases/CELLS.db" ] && [ -f "databases/CELLS.db" ]; then
    echo "Copying binary database files..."
    cp databases/CELLS.db* . 2>/dev/null || true
    cp databases/transactions.db . 2>/dev/null || true
    chmod 644 *.db* 2>/dev/null || true
fi

echo "Databases imported"
echo ""

# Build the project
echo "Building CELLS..."
cd source
go build -o ../CELLS ./cmd/CELLS
cd ..
echo "Build complete"
echo ""

# Verify installation
echo "Verifying installation..."
if [ -f "CELLS" ]; then
    echo "鉁?CELLS binary created successfully"
else
    echo "鉁?CELLS binary not found"
fi

if [ -f "databases/CELLS.db" ] || [ -f "CELLS.db" ]; then
    echo "鉁?Database files present"
    if command -v sqlite3 >/dev/null 2>&1; then
        TX_COUNT=sqlite3 databases/CELLS.db "SELECT COUNT(*) FROM transactions;" 2>/dev/null || sqlite3 CELLS.db "SELECT COUNT(*) FROM transactions;" 2>/dev/null || echo "0"
        echo "  Transactions in database: $TX_COUNT"
    fi
else
    echo "鉁?Database files not found"
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "1. Review and update configuration files in config/"
echo "2. Test the installation: ./CELLS --help"
echo "3. Start the service as needed"
echo ""
