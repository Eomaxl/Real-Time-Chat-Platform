# Load Testing Makefile

# Default configuration
API_URL ?= http://localhost:8080
WS_URL ?= ws://localhost:8080
TEST_DURATION ?= 5m
VUS ?= 100

# Export environment variables
export API_URL
export WS_URL
export TEST_DURATION
export VUS

.PHONY: all chat websocket presence call e2e regression hyperscale hyperscale-ws hyperscale-throughput hyperscale-db clean help

# Run all load tests
all: chat websocket presence call e2e regression

# Run all hyperscale validation tests
hyperscale: hyperscale-ws hyperscale-throughput hyperscale-db

# Individual test targets
chat:
	@echo "Running chat load test..."
	k6 run chat-load-test.js

websocket:
	@echo "Running WebSocket load test..."
	k6 run websocket-load-test.js

presence:
	@echo "Running presence load test..."
	k6 run presence-load-test.js

call:
	@echo "Running call load test..."
	k6 run call-load-test.js

e2e:
	@echo "Running end-to-end workflow test..."
	k6 run e2e-workflow-test.js

regression:
	@echo "Running performance regression test..."
	k6 run regression-test.js

# Hyperscale validation tests
hyperscale-ws:
	@echo "Running hyperscale WebSocket test (50K connections)..."
	k6 run hyperscale-websocket-test.js

hyperscale-throughput:
	@echo "Running hyperscale throughput test (2M messages/sec)..."
	k6 run hyperscale-throughput-test.js

hyperscale-db:
	@echo "Running hyperscale database test..."
	k6 run hyperscale-database-test.js

# Quick smoke test (low load)
smoke:
	@echo "Running smoke tests..."
	VUS=10 TEST_DURATION=1m k6 run chat-load-test.js
	VUS=10 TEST_DURATION=1m k6 run presence-load-test.js

# Stress test (high load)
stress:
	@echo "Running stress tests..."
	VUS=1000 TEST_DURATION=10m k6 run chat-load-test.js
	VUS=5000 TEST_DURATION=10m k6 run websocket-load-test.js

# Spike test (sudden load increase)
spike:
	@echo "Running spike test..."
	k6 run --stage 1m:100 --stage 30s:1000 --stage 1m:100 --stage 1m:0 chat-load-test.js

# Soak test (extended duration)
soak:
	@echo "Running soak test (1 hour)..."
	TEST_DURATION=1h VUS=500 k6 run chat-load-test.js

# Clean up test artifacts
clean:
	@echo "Cleaning up test artifacts..."
	rm -f regression-report.txt summary.json

# Help
help:
	@echo "Load Testing Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all                - Run all load tests"
	@echo "  chat               - Run chat message load test"
	@echo "  websocket          - Run WebSocket connection test"
	@echo "  presence           - Run presence service test"
	@echo "  call               - Run call service test"
	@echo "  e2e                - Run end-to-end workflow test"
	@echo "  regression         - Run performance regression test"
	@echo "  hyperscale         - Run all hyperscale validation tests"
	@echo "  hyperscale-ws      - Validate 50K WebSocket connections (Req 8.3)"
	@echo "  hyperscale-throughput - Validate 2M messages/sec throughput (Req 8.1, 8.2)"
	@echo "  hyperscale-db      - Validate database performance (Req 8.3)"
	@echo "  smoke              - Run quick smoke tests (low load)"
	@echo "  stress             - Run stress tests (high load)"
	@echo "  spike              - Run spike test (sudden load)"
	@echo "  soak               - Run soak test (extended duration)"
	@echo "  clean              - Clean up test artifacts"
	@echo "  help               - Show this help message"
	@echo ""
	@echo "Environment Variables:"
	@echo "  API_URL         - API Gateway URL (default: http://localhost:8080)"
	@echo "  WS_URL          - WebSocket URL (default: ws://localhost:8080)"
	@echo "  TEST_DURATION   - Test duration (default: 5m)"
	@echo "  VUS             - Number of virtual users (default: 100)"
	@echo ""
	@echo "Examples:"
	@echo "  make chat"
	@echo "  API_URL=https://api.example.com make all"
	@echo "  VUS=500 TEST_DURATION=10m make stress"
	@echo "  make hyperscale  # Run all hyperscale validation tests"
