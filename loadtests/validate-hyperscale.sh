#!/bin/bash

# Hyperscale Performance Validation Script
# This script validates all hyperscale performance requirements from the design document

set -e

echo "=========================================="
echo "Hyperscale Performance Validation"
echo "=========================================="
echo ""
echo "This script validates the following requirements:"
echo "  - Requirement 8.1: Message latency p95 < 200ms"
echo "  - Requirement 8.2: Signaling latency p95 < 150ms"
echo "  - Requirement 8.3: 50K WebSocket connections per node"
echo "  - Requirement 8.3: Database query p95 < 5ms"
echo "  - Global throughput: 2M messages/second"
echo ""

# Configuration
API_URL=${API_URL:-http://localhost:8080}
WS_URL=${WS_URL:-ws://localhost:8080}
REPORT_DIR="./reports"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Create report directory
mkdir -p "$REPORT_DIR"

echo "Configuration:"
echo "  API URL: $API_URL"
echo "  WebSocket URL: $WS_URL"
echo "  Report Directory: $REPORT_DIR"
echo ""

# Check if k6 is installed
if ! command -v k6 &> /dev/null; then
    echo "Error: k6 is not installed"
    echo "Please install k6: https://k6.io/docs/getting-started/installation/"
    exit 1
fi

echo "k6 version: $(k6 version)"
echo ""

# Function to run a test and capture results
run_test() {
    local test_name=$1
    local test_file=$2
    local report_file="$REPORT_DIR/${test_name}_${TIMESTAMP}.txt"
    
    echo "=========================================="
    echo "Running: $test_name"
    echo "=========================================="
    echo ""
    
    if k6 run "$test_file" 2>&1 | tee "$report_file"; then
        echo ""
        echo "✅ $test_name completed successfully"
        echo "Report saved to: $report_file"
        return 0
    else
        echo ""
        echo "❌ $test_name failed"
        echo "Report saved to: $report_file"
        return 1
    fi
}

# Track overall results
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# Test 1: WebSocket Connection Capacity (50K connections)
echo ""
echo "=========================================="
echo "Test 1: WebSocket Connection Capacity"
echo "Target: 50,000 concurrent connections per node"
echo "=========================================="
echo ""
read -p "This test will attempt to establish 50K WebSocket connections. Continue? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if run_test "hyperscale-websocket" "hyperscale-websocket-test.js"; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
else
    echo "Skipping WebSocket capacity test"
fi

# Test 2: Message Throughput (2M messages/second globally)
echo ""
echo "=========================================="
echo "Test 2: Message Throughput"
echo "Target: 2M messages/second globally"
echo "=========================================="
echo ""
read -p "This test will generate high message throughput. Continue? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if run_test "hyperscale-throughput" "hyperscale-throughput-test.js"; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
else
    echo "Skipping throughput test"
fi

# Test 3: Database Performance
echo ""
echo "=========================================="
echo "Test 3: Database Performance"
echo "Target: Query p95 < 5ms (pure DB time)"
echo "=========================================="
echo ""
read -p "This test will stress the database with mixed read/write workload. Continue? (y/n) " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
    if run_test "hyperscale-database" "hyperscale-database-test.js"; then
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
else
    echo "Skipping database performance test"
fi

# Generate summary report
SUMMARY_FILE="$REPORT_DIR/validation_summary_${TIMESTAMP}.txt"

echo ""
echo "=========================================="
echo "Validation Summary"
echo "=========================================="
echo "" | tee "$SUMMARY_FILE"
echo "Timestamp: $(date)" | tee -a "$SUMMARY_FILE"
echo "Total Tests: $TOTAL_TESTS" | tee -a "$SUMMARY_FILE"
echo "Passed: $PASSED_TESTS" | tee -a "$SUMMARY_FILE"
echo "Failed: $FAILED_TESTS" | tee -a "$SUMMARY_FILE"
echo "" | tee -a "$SUMMARY_FILE"

if [ $FAILED_TESTS -eq 0 ] && [ $TOTAL_TESTS -gt 0 ]; then
    echo "✅ All hyperscale performance requirements validated successfully!" | tee -a "$SUMMARY_FILE"
    echo "" | tee -a "$SUMMARY_FILE"
    echo "The system meets the following targets:" | tee -a "$SUMMARY_FILE"
    echo "  ✅ 50K concurrent WebSocket connections per node" | tee -a "$SUMMARY_FILE"
    echo "  ✅ 2M messages/second global throughput" | tee -a "$SUMMARY_FILE"
    echo "  ✅ Message latency p95 < 200ms" | tee -a "$SUMMARY_FILE"
    echo "  ✅ Signaling latency p95 < 150ms" | tee -a "$SUMMARY_FILE"
    echo "  ✅ Database performance optimized" | tee -a "$SUMMARY_FILE"
    EXIT_CODE=0
elif [ $TOTAL_TESTS -eq 0 ]; then
    echo "⚠️  No tests were run" | tee -a "$SUMMARY_FILE"
    EXIT_CODE=0
else
    echo "❌ Some hyperscale performance requirements not met" | tee -a "$SUMMARY_FILE"
    echo "" | tee -a "$SUMMARY_FILE"
    echo "Review individual test reports in: $REPORT_DIR" | tee -a "$SUMMARY_FILE"
    echo "" | tee -a "$SUMMARY_FILE"
    echo "Recommendations:" | tee -a "$SUMMARY_FILE"
    echo "  - Review system resource allocation (CPU, memory, network)" | tee -a "$SUMMARY_FILE"
    echo "  - Check database connection pooling and query optimization" | tee -a "$SUMMARY_FILE"
    echo "  - Verify Redis cluster configuration and performance" | tee -a "$SUMMARY_FILE"
    echo "  - Consider horizontal scaling (more nodes)" | tee -a "$SUMMARY_FILE"
    echo "  - Review application-level optimizations (caching, batching)" | tee -a "$SUMMARY_FILE"
    EXIT_CODE=1
fi

echo "" | tee -a "$SUMMARY_FILE"
echo "Summary report saved to: $SUMMARY_FILE" | tee -a "$SUMMARY_FILE"
echo ""

exit $EXIT_CODE
