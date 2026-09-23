export PATH := $(HOME)/.local/go/bin:$(PATH)

.PHONY: build test vet run-identity docker-identity clean

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

vet:
	go vet ./...

# Local dev server: data in ./data, verification codes printed in the log.
run-identity: build
	QUAREL_ISSUER=localhost:8080 ./bin/quarel-identity

docker-identity:
	docker build -f Dockerfile.identity -t quarel-identity .

clean:
	rm -rf bin
