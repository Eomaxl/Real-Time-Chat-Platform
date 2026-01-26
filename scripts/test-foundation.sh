#!/bin/bash

# Foundation test script for Real-Time Chat Platform
# This script validates that all foundation components are working correctly

set -e

echo "🧪 Testing Real-Time Chat Platform Foundation"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

print_header() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

# Test results
TESTS_PASSED=0
TESTS_FAILED=0

# Function to run a test
run_test() {
    local test_name="$1"
    local test_command="$2"
    
    print_header "$test_name"
    
    if eval "$test_command" > /dev/null 2>&1; then
        print_status "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        print_error "$test_name"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# Create necessary directories
mkdir -p logs pids

# Test 1: Build all services
print_header "Building all services..."
if make build > logs/build.log 2>&1; then
    print_status "All services built successfully"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    print_error "Build failed - check logs/build.log"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    exit 1
fi

# Test 2: Start infrastructure
print_header "Starting infrastructure services..."
docker-compose up -d postgres redis > logs/docker.log 2>&1

# Wait for services to be ready
print_header "Waiting for infrastructure to be ready..."
sleep 10

# Test PostgreSQL
run_test "PostgreSQL connectivity" "docker-compose exec -T postgres pg_isready -U postgres"

# Test Redis
run_test "Redis connectivity" "docker-compose exec -T redis redis-cli ping"

# Test 3: Start and test each service
services=("api-gateway:8080" "chat-service:8081" "presence-service:8082" "call-service:8083")

for service_info in "${services[@]}"; do
    IFS=':' read -r service_name port <<< "$service_info"
    
    print_header "Testing $service_name on port $port"
    
    # Start service in background
    if [ "$service_name" = "api-gateway" ]; then
        ./bin/api-gateway > "logs/${service_name}.log" 2>&1 &
    else
        env "${service_name^^}_PORT=:$port" "./bin/$service_name" > "logs/${service_name}.log" 2>&1 &
    fi
    
    service_pid=$!
    echo $service_pid > "pids/${service_name}.pid"
    
    # Wait for service to start
    sleep 3
    
    # Test health endpoint
    if run_test "$service_name health check" "curl -s http://localhost:$port/health"; then
        # Test that health response contains expected fields
        health_response=$(curl -s "http://localhost:$port/health")
        if echo "$health_response" | grep -q '"status":"healthy"'; then
            print_status "$service_name returns healthy status"
            TESTS_PASSED=$((TESTS_PASSED + 1))
        else
            print_error "$service_name does not return healthy status"
            TESTS_FAILED=$((TESTS_FAILED + 1))
        fi
    fi
    
    # Test readiness endpoint
    run_test "$service_name readiness check" "curl -s http://localhost:$port/health/ready"
    
    # Test liveness endpoint
    run_test "$service_name liveness check" "curl -s http://localhost:$port/health/live"
    
    # Test metrics endpoint
    run_test "$service_name metrics endpoint" "curl -s http://localhost:$port/metrics"
done

# Test 4: Test service-specific endpoints
print_header "Testing service-specific endpoints..."

# Test Chat Service
run_test "Chat service GET messages" "curl -s http://localhost:8081/v1/messages"
run_test "Chat service POST messages" "curl -X POST -s http://localhost:8081/v1/messages"

# Test Presence Service
run_test "Presence service heartbeat" "curl -X POST -s http://localhost:8082/v1/heartbeat"
run_test "Presence service get presence" "curl -s http://localhost:8082/v1/presence/test-user"

# Test Call Service
run_test "Call service create call" "curl -X POST -s http://localhost:8083/v1/calls"
run_test "Call service join call" "curl -X POST -s http://localhost:8083/v1/calls/test-call/join"

# Test 5: Database schema validation
print_header "Validating database schema..."
if docker-compose exec -T postgres psql -U postgres -d chatplatform -c "\dt" | grep -q "messages"; then
    print_status "Database schema contains required tables"
    TESTS_PASSED=$((TESTS_PASSED + 1))
else
    print_error "Database schema missing required tables"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Test 6: Redis functionality
print_header "Testing Redis functionality..."
if docker-compose exec -T redis redis-cli set test_key test_value | grep -q "OK"; then
    if docker-compose exec -T redis redis-cli get test_key | grep -q "test_value"; then
        print_status "Redis read/write operations working"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        docker-compose exec -T redis redis-cli del test_key > /dev/null
    else
        print_error "Redis read operation failed"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
else
    print_error "Redis write operation failed"
    TESTS_FAILED=$((TESTS_FAILED + 1))
fi

# Cleanup
print_header "Cleaning up test environment..."

# Stop all services
for service_info in "${services[@]}"; do
    IFS=':' read -r service_name port <<< "$service_info"
    pid_file="pids/${service_name}.pid"
    
    if [ -f "$pid_file" ]; then
        pid=$(cat "$pid_file")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
        fi
        rm -f "$pid_file"
    fi
done

# Stop Docker services
docker-compose down > /dev/null 2>&1

# Test Results
echo ""
echo "📊 Test Results:"
echo "  • Tests Passed: $TESTS_PASSED"
echo "  • Tests Failed: $TESTS_FAILED"
echo "  • Total Tests:  $((TESTS_PASSED + TESTS_FAILED))"

if [ $TESTS_FAILED -eq 0 ]; then
    echo ""
    print_status "🎉 All foundation tests passed! The infrastructure is ready."
    exit 0
else
    echo ""
    print_error "❌ Some tests failed. Check the logs in the logs/ directory."
    exit 1
fi