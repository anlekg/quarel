export PATH := $(HOME)/.local/go/bin:$(HOME)/.local/bin:$(PATH)

.PHONY: build test test-race vet run-identity run-server docker-identity docker-server e2e-voice e2e-dm clean

build:
	go build -o bin/ ./cmd/...

test:
	go test -timeout 120s ./...

# Race detector (needs gcc): use after touching concurrent code (gateway hub…).
test-race:
	CGO_ENABLED=1 go test -race -timeout 300s ./...

vet:
	go vet ./...

# Local dev Identity service on :8080, data in ./data/identity, verification codes printed in the log.
run-identity: build
	QUAREL_ISSUER=localhost:8080 QUAREL_DATA_DIR=./data/identity ./bin/quarel-identity

# Local dev community server on :8090, trusting the local Identity service, data in ./data/server.
# Voice uses livekit-server from PATH (~/.local/bin); ports 7880 (loopback), 7881/tcp, 7882/udp.
run-server: build
	QUAREL_TRUSTED_ISSUERS=localhost:8080 QUAREL_DATA_DIR=./data/server ./bin/quarel-server

docker-identity:
	docker build -f Dockerfile.identity -t quarel-identity .

docker-server:
	docker build -f Dockerfile.server -t quarel-server .

clean:
	rm -rf bin

# End-to-end voice test with real browsers (installs Playwright + Chromium on first run).
e2e-voice: build
	cd test/e2e && [ -d node_modules ] || (npm install --silent && npx playwright install chromium)
	test/e2e/voice.sh

# End-to-end encrypted DM scenario (Identity service + quarelctl).
e2e-dm: build
	test/e2e/dm.sh
