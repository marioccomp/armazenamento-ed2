BIN    := bin/engine
GEN    := bin/gen
IMAGE  := ed2-engine

.PHONY: all build test race vet fmt check crashtest bench docker clean

all: check build

build:
	go build -o $(BIN) ./cmd/engine
	go build -o $(GEN) ./cmd/gen

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# Usado antes de cada commit: formatação, vet e testes.
check:
	@test -z "$$(gofmt -l .)" || (echo "arquivos sem gofmt:"; gofmt -l .; exit 1)
	go vet ./...
	go test ./...

crashtest: build
	./scripts/crashtest.sh

bench: build
	./scripts/bench.sh

docker:
	docker build -t $(IMAGE) .

clean:
	rm -rf bin
