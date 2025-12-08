IMAGE_NAME := joelmathew357/krs
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: all build run test fmt vet docker-build docker-push clean

all: fmt vet build

build:
	@echo "Building controller..."
	@mkdir -p bin
	go build $(LDFLAGS) -o bin/controller ./cmd/controller/

run:
	@echo "Running locally..."
	go run $(LDFLAGS) ./cmd/controller/

test:
	@echo "Running tests..."
	go test -v ./...

fmt:
	@echo "Formatting code..."
	go fmt ./...

vet:
	@echo "Running static analysis..."
	go vet ./...

docker-build:
	@echo "Building Docker image $(IMAGE_NAME):$(VERSION)..."
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE_NAME):$(VERSION) .
	docker tag $(IMAGE_NAME):$(VERSION) $(IMAGE_NAME):latest

docker-push: docker-build
	@echo "Pushing Docker image..."
	docker push $(IMAGE_NAME):$(VERSION)
	docker push $(IMAGE_NAME):latest

clean:
	@echo "Cleaning up..."
	rm -rf bin
