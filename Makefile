IMG ?= sensors:latest

all: lint
	go build -o sensors

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet tidy

test: lint
	go test ./...

docker-build: test
	docker build . -t ${IMG}

docker-push:
	docker push ${IMG}

