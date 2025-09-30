#!/bin/bash

# Bitcoin Core daemon management script
# Usage: ./bitcoind_manager.sh [start|stop|status]

DATADIR="$(pwd)/bitcoind_regtest"
RPC_PORT="18443"
RPC_USER="user"
RPC_PASS="pass"

# Function to check if bitcoind is running
is_running() {
    if lsof -ti:$RPC_PORT >/dev/null 2>&1; then
        return 0  # Running
    else
        return 1  # Not running
    fi
}

# Function to start bitcoind
start_bitcoind() {
    if is_running; then
        echo "ERROR: bitcoind is already running on port $RPC_PORT"
        exit 1
    fi
    
    # Clean up existing datadir
    if [ -d "$DATADIR" ]; then
        echo "Cleaning up existing datadir..."
        rm -rf "$DATADIR"
    fi
    
    # Create datadir
    mkdir -p "$DATADIR"
    
    # Start bitcoind
    echo "Starting bitcoind in regtest mode..."
    bitcoind \
        -regtest \
        -datadir="$DATADIR" \
        -server \
        -rpcuser="$RPC_USER" \
        -rpcpassword="$RPC_PASS" \
        -rpcport="$RPC_PORT" \
        -rpcbind=127.0.0.1 \
        -rpcallowip=127.0.0.1 \
        -fallbackfee=0.0002 \
        -txindex \
        -daemon
    
    # Wait for bitcoind to be ready
    echo "Waiting for bitcoind to be ready..."
    for i in {1..20}; do
        if bitcoin-cli -regtest -rpcuser="$RPC_USER" -rpcpassword="$RPC_PASS" -rpcport="$RPC_PORT" getblockcount >/dev/null 2>&1; then
            echo "bitcoind is ready!"
            exit 0
        fi
        sleep 0.5
    done
    
    echo "ERROR: bitcoind failed to start properly"
    exit 1
}

# Function to stop bitcoind
stop_bitcoind() {
    if ! is_running; then
        echo "bitcoind is not running"
        # Clean up datadir anyway
        if [ -d "$DATADIR" ]; then
            echo "Cleaning up datadir..."
            rm -rf "$DATADIR"
        fi
        exit 0
    fi
    
    echo "Stopping bitcoind..."
    
    # Try graceful shutdown via RPC
    if bitcoin-cli -regtest -rpcuser="$RPC_USER" -rpcpassword="$RPC_PASS" -rpcport="$RPC_PORT" stop >/dev/null 2>&1; then
        echo "Sent stop command via RPC"
        sleep 3
    fi
    
    # Check if still running
    if is_running; then
        echo "Force killing bitcoind..."
        # Get PID and kill
        PID=$(lsof -ti:$RPC_PORT)
        if [ ! -z "$PID" ]; then
            kill -TERM "$PID" 2>/dev/null
            sleep 2
            if is_running; then
                kill -KILL "$PID" 2>/dev/null
                sleep 1
            fi
        fi
    fi
    
    # Clean up datadir
    if [ -d "$DATADIR" ]; then
        echo "Cleaning up datadir..."
        rm -rf "$DATADIR"
    fi
    
    echo "bitcoind stopped"
}

# Function to check status
status_bitcoind() {
    if is_running; then
        echo "bitcoind is running on port $RPC_PORT"
        if [ -d "$DATADIR" ]; then
            echo "datadir: $DATADIR"
        fi
    else
        echo "bitcoind is not running"
    fi
}

# Main script logic
case "$1" in
    start)
        start_bitcoind
        ;;
    stop)
        stop_bitcoind
        ;;
    status)
        status_bitcoind
        ;;
    *)
        echo "Usage: $0 {start|stop|status}"
        exit 1
        ;;
esac