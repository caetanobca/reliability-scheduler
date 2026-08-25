.PHONY: build clean test fmt vet install deps help docker-build docker-push deploy undeploy

# Binary name
BINARY_NAME=reliability-scheduler

# Build directory
BUILD_DIR=./bin

# Docker image settings
REGISTRY ?= docker.io/seu-usuario
IMAGE_NAME ?= reliability-scheduler
VERSION ?= v1.0.0
IMAGE_TAG = $(REGISTRY)/$(IMAGE_NAME):$(VERSION)

# Deployment mode: standalone or integrated
MODE ?= integrated
DEPLOY_DIR = deploy/$(MODE)

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet
GOMOD=$(GOCMD) mod

# Build the project
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/scheduler

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Install the binary
install: build
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

# Run all checks
check: fmt vet test

# Build Docker image
docker-build:
	@echo "Building Docker image $(IMAGE_TAG)..."
	docker build -t $(IMAGE_TAG) .

# Push Docker image
docker-push: docker-build
	@echo "Pushing Docker image $(IMAGE_TAG)..."
	docker push $(IMAGE_TAG)

# Update deployment with image tag
update-deployment-image:
	@echo "Updating deployment with image $(IMAGE_TAG) (mode: $(MODE))..."
	@sed -i.bak "s|image: .*|image: $(IMAGE_TAG)|g" $(DEPLOY_DIR)/deployment.yaml
	@rm -f $(DEPLOY_DIR)/deployment.yaml.bak

# Deploy to Kubernetes
deploy: update-deployment-image
	@echo "Deploying to Kubernetes (mode: $(MODE))..."
	@if [ ! -d "$(DEPLOY_DIR)" ]; then \
		echo "Error: Directory $(DEPLOY_DIR) does not exist"; \
		echo "Valid modes: standalone, integrated"; \
		exit 1; \
	fi
	kubectl apply -f $(DEPLOY_DIR)/rbac.yaml
	kubectl apply -f $(DEPLOY_DIR)/configmap.yaml
	kubectl apply -f $(DEPLOY_DIR)/deployment.yaml
	@echo "Waiting for deployment to be ready..."
	kubectl wait --for=condition=available --timeout=120s deployment/reliability-scheduler -n kube-scheduler-reliability

# Deploy everything (build, push, deploy)
deploy-all: docker-push deploy
	@echo "Full deployment completed (mode: $(MODE))!"
	@echo "Verifying deployment..."
	kubectl get pods -n kube-scheduler-reliability

# Undeploy from Kubernetes
undeploy:
	@echo "Removing deployment from Kubernetes (mode: $(MODE))..."
	@if [ ! -d "$(DEPLOY_DIR)" ]; then \
		echo "Error: Directory $(DEPLOY_DIR) does not exist"; \
		echo "Valid modes: standalone, integrated"; \
		exit 1; \
	fi
	kubectl delete -f $(DEPLOY_DIR)/deployment.yaml --ignore-not-found
	kubectl delete -f $(DEPLOY_DIR)/configmap.yaml --ignore-not-found
	kubectl delete -f $(DEPLOY_DIR)/rbac.yaml --ignore-not-found

# View logs
logs:
	@echo "Fetching logs from scheduler..."
	kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler --tail=100

# Follow logs
logs-follow:
	@echo "Following logs from scheduler..."
	kubectl logs -n kube-scheduler-reliability -l app=reliability-scheduler -f

# Test deployment
test-deploy:
	@echo "Deploying test pod (mode: $(MODE))..."
	kubectl apply -f $(DEPLOY_DIR)/test-pod.yaml
	@echo "Waiting for pod to be scheduled..."
	@sleep 5
	kubectl get pod test-reliability-scheduler -o wide
	kubectl describe pod test-reliability-scheduler
	@echo "\nCleaning up test pod..."
	kubectl delete -f $(DEPLOY_DIR)/test-pod.yaml

# Display help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build & Test:"
	@echo "  build          - Build the project"
	@echo "  clean          - Clean build artifacts"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage"
	@echo "  fmt            - Format code"
	@echo "  vet            - Run go vet"
	@echo "  deps           - Download dependencies"
	@echo "  check          - Run fmt, vet, and test"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build   - Build Docker image"
	@echo "  docker-push    - Build and push Docker image"
	@echo ""
	@echo "Kubernetes:"
	@echo "  deploy         - Deploy to Kubernetes (MODE=integrated|standalone)"
	@echo "  deploy-all     - Build, push, and deploy"
	@echo "  undeploy       - Remove from Kubernetes"
	@echo "  test-deploy    - Deploy test pod and verify"
	@echo "  logs           - View scheduler logs"
	@echo "  logs-follow    - Follow scheduler logs"
	@echo ""
	@echo "Deployment Modes:"
	@echo "  MODE=integrated  - Deploy with all K8s plugins (default, recommended)"
	@echo "  MODE=standalone  - Deploy only ReliabilityScheduler"
	@echo ""
	@echo "Usage examples:"
	@echo "  # Deploy integrated mode (recommended)"
	@echo "  make deploy-all REGISTRY=gcr.io/my-project VERSION=v1.0.1"
	@echo ""
	@echo "  # Deploy standalone mode"
	@echo "  make deploy-all MODE=standalone REGISTRY=gcr.io/my-project VERSION=v1.0.1"
	@echo ""
	@echo "  # Just deploy (image already exists)"
	@echo "  make deploy MODE=integrated REGISTRY=gcr.io/my-project VERSION=v1.0.1"

# Default target
.DEFAULT_GOAL := help
