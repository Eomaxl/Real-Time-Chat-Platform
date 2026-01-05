

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Binary Names
GATEWAY_BINARY=bin/api-gateway
CHAT_BINARY=bin/chat-service
PRESENCE_BINARY=bin/presence-service
CALL_BINARY=bin/call-service

# Setup development environment
setup:
	mkdir -p logs pids bin
	chmod +x scripts/start-dev.sh scripts/stop-dev.sh

# Build all services
build: setup build-gateway build-chat build-presence build-call

build-gateway:
	$(GOBUILD -o $(GATEWAY_BINARY) ./cmd/api-gateway

build-chat:
	$(GOBUILD) -o $(CHAT_BINARY) ./cmd/chat-service

build-presence:
	$(GOBUILD) -o $(PRESENCE_BINARY) ./cmd/presence-service

build-call:
	$(GOBUILD) -o $(CALL_BINARY) .cmd/call-service

# Run services (for development)
run-gateway:
	$(GOCMD) run ./cmd/api-gateway

run-chat:
	CHAT_PORT=:8081 $(GOCMD) run ./cmd/chat-service

run-presence:
	PRESENCE_PORT:=8082 $(GOCMD) run ./cmd/presence-service

run-call:
	CALL_PORT:=8083$(GOCMD) run ./cmd/call-service

# Development environment
dev-start: build
	./scripts/start-dev.sh

dev-stop:
	./scripts/stop-dev.sh

# Test
test:
	$(GOTEST) -v ./...

test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out

# Clean
clean:
	$(GOCLEAN)
	rm -rf bin/ logs/ pids/

# Dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Docker commands
docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f

# Database commands
db-migrate:
	@echo "Database migration would run here"

db-seed:
	@echo "Database seeding would run here"

# Development setup
dev-setup: deps setup docker-up
	@echo "Development environment ready"

# Production build
prod-build: setup
CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -o $(GATEWAY_BINARY) ./cmd/api-gateway
CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -o $(CHAT_BINARY) ./cmd/chat-service
CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -o $(PRESENCE_BINARY) ./cmd/presence-service
CGO_ENABLED=0 GOOS=linux $(GOBUILD) -a -installsuffix cgo -o $(CALL_BINARY) ./cmd/call-service
