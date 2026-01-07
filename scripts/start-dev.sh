#!/bin/bash

# Development startup script for Real-Time Chat Platform
# This script starts all services in development mode

set -e

echo "🚀 Starting Real-Time Chat Platform Development Environment"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}[STEP]${NC} $1"
}

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    print_error "Docker is not running. Please start Docker first."
    exit 1
fi

# Step 1: Start infrastructure services
print_header "Starting infrastructure services (PostgreSQL, Redis)..."
docker-compose up -d postgres redis

# Wait for services to be ready
print_status "Waiting for PostgreSQL to be ready..."
until docker-compose exec postgres pg_isready -U postgres > /dev/null 2>&1; do
    sleep 1
done

print_status "Waiting for Redis to be ready..."
until docker-compose exec redis redis-cli ping > /dev/null 2>&1; do
    sleep 1
done

print_status "Infrastructure services are ready!"

# Step 2: Build Go services
print_header "Building Go services..."
make build

# Step 3: Start services in background
print_header "Starting microservices..."

# Start API Gateway
print_status "Starting API Gateway on :8080..."
GATEWAY_PORT=:8080 ./bin/api-gateway > logs/gateway.log 2>&1 &
GATEWAY_PID=$!
echo $GATEWAY_PID > pids/gateway.pid

# Start Chat Service
print_status "Starting Chat Service on :8081..."
CHAT_PORT=:8081 ./bin/chat-service > logs/chat.log 2>&1 &
CHAT_PID=$!
echo $CHAT_PID > pids/chat.pid

# Start Presence Service
print_status "Starting Presence Service on :8082..."
PRESENCE_PORT=:8082 ./bin/presence-service > logs/presence.log 2>&1 &
PRESENCE_PID=$!
echo $PRESENCE_PID > pids/presence.pid

# Start Call Service
print_status "Starting Call Service on :8083..."
CALL_PORT=:8083 ./bin/call-service > logs/call.log 2>&1 &
CALL_PID=$!
echo $CALL_PID > pids/call.pid

# Wait a moment for services to start
sleep 3

# Step 4: Health checks
print_header "Performing health checks..."

check_service() {
    local service_name=$1
    local port=$2
    local max_attempts=10
    local attempt=1

    while [ $attempt -le $max_attempts ]; do
        if curl -s "http://localhost:$port/health" > /dev/null 2>&1; then
            print_status "$service_name is healthy ✓"
            return 0
        fi
        print_warning "$service_name health check attempt $attempt/$max_attempts failed, retrying..."
        sleep 2
        attempt=$((attempt + 1))
    done

    print_error "$service_name failed health checks after $max_attempts attempts"
    return 1
}

# Check all services
check_service "API Gateway" "8080"
check_service "Chat Service" "8081"
check_service "Presence Service" "8082"
check_service "Call Service" "8083"

# Step 5: Display service information
print_header "Development environment is ready!"

echo ""
echo "📋 Service Information:"
echo "  • API Gateway:     http://localhost:8080"
echo "  • Chat Service:    http://localhost:8081"
echo "  • Presence Service: http://localhost:8082"
echo "  • Call Service:    http://localhost:8083"
echo ""
echo "🔍 Health Endpoints:"
echo "  • Gateway Health:  http://localhost:8080/health"
echo "  • Chat Health:     http://localhost:8081/health"
echo "  • Presence Health: http://localhost:8082/health"
echo "  • Call Health:     http://localhost:8083/health"
echo ""
echo "📊 Infrastructure:"
echo "  • PostgreSQL:      localhost:5432 (user: postgres, db: chatplatform)"
echo "  • Redis:           localhost:6379"
echo ""
echo "📝 Logs:"
echo "  • Gateway:         tail -f logs/gateway.log"
echo "  • Chat:            tail -f logs/chat.log"
echo "  • Presence:        tail -f logs/presence.log"
echo "  • Call:            tail -f logs/call.log"
echo ""
echo "🛑 To stop all services: ./scripts/stop-dev.sh"
echo ""

print_status "All services are running! 🎉"