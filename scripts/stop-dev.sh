#!/bin/bash

# Development stop script for Real-Time Chat Platform
# This script stops all services

set -e

echo "🛑 Stopping Real-Time Chat Platform Development Environment"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Create directories if they don't exist
mkdir -p pids logs

# Stop Go services
print_status "Stopping Go services..."

stop_service() {
    local service_name=$1
    local pid_file="pids/${service_name}.pid"
    
    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            print_status "Stopping $service_name (PID: $pid)..."
            kill "$pid"
            rm -f "$pid_file"
        else
            print_warning "$service_name was not running"
            rm -f "$pid_file"
        fi
    else
        print_warning "No PID file found for $service_name"
    fi
}

stop_service "gateway"
stop_service "chat"
stop_service "presence"
stop_service "call"

# Stop Docker services
print_status "Stopping Docker services..."
docker-compose down

print_status "All services stopped! 🏁"