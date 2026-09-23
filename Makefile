export PATH := $(HOME)/.local/go/bin:$(PATH)

.PHONY: build test test-race vet run-identity run-server docker-identity docker-server clean

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

# Race detector (needs gcc): use after touching concurrent code (gateway hub…).
test-race:
	CGO_ENABLED=1 go test -race ./...

vet:
	go vet ./...

# Local dev Identity service on :8080, data in ./data/identity, verification codes printed in the log.
run-identity: build
	QUAREL_ISSUER=localhost:8080 QUAREL_DATA_DIR=./data/identity ./bin/quarel-identity

# Local dev community server on :8090, trusting the local Identity service, data in ./data/server.
run-server: build
	QUAREL_TRUSTED_ISSUERS=localhost:8080 QUAREL_DATA_DIR=./data/server ./bin/quarel-server

docker-identity:
	docker build -f Dockerfile.identity -t quarel-identity .

docker-server:
	docker build -f Dockerfile.server -t quarel-server .

clean:
	rm -rf bin
