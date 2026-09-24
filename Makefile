export PATH := $(HOME)/.local/go/bin:$(HOME)/.local/bin:$(PATH)

.PHONY: build test test-race vet run-identity run-server docker-identity docker-server e2e-voice e2e-dm e2e-security e2e-acme e2e-messages e2e-moderation e2e-accounts e2e-dm-groups e2e-calls e2e-ops windows client-install client-dev client-build client-test e2e-client clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/anlekg/quarel/internal/backup.Release=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/ ./cmd/... ./examples/...

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

# Local dev community server on https://localhost:8090 (self-signed), trusting the local Identity
# service, data in ./data/server. UPnP off (never touch the router from dev), voice on local addresses.
# Voice uses livekit-server from PATH (~/.local/bin); ports 7880 (loopback), 7881/tcp, 7882/udp.
run-server: build
	QUAREL_TRUSTED_ISSUERS=localhost:8080 QUAREL_DATA_DIR=./data/server QUAREL_UPNP=off QUAREL_VOICE_PUBLIC_IP=local ./bin/quarel-server

docker-identity:
	docker build --build-arg VERSION=$(VERSION) -f Dockerfile.identity -t quarel-identity .

docker-server:
	docker build --build-arg VERSION=$(VERSION) -f Dockerfile.server -t quarel-server .

clean:
	rm -rf bin

# End-to-end voice test with real browsers (installs Playwright + Chromium on first run).
e2e-voice: build
	cd test/e2e && [ -d node_modules ] || (npm install --silent && npx playwright install chromium)
	test/e2e/voice.sh

# End-to-end encrypted DM scenario (Identity service + quarelctl).
e2e-dm: build
	test/e2e/dm.sh

# Milestone 6 scenario: recovery phrase, TLS identity pinning, rate limiting.
e2e-security: build
	test/e2e/security.sh

# P1 scenario: replies, reactions, pins, files, search, threads, unread state.
e2e-messages: build
	test/e2e/messages.sh

# P1 scenario: timeout, purge, audit log, rules, phone verification, bots.
e2e-moderation: build
	test/e2e/moderation.sh

# P1 scenario: account changes, profile, blocks, presence, deletion, operator tool.
e2e-accounts: build
	test/e2e/accounts.sh

# P1 scenario: groups, edits and deletions, encrypted files, typing, read receipts.
e2e-dm-groups: build
	test/e2e/dm-groups.sh

# P1 scenario: peer-to-peer calls, direct and through the TURN relay.
e2e-calls: build
	test/e2e/calls.sh

# P1 scenario: live backups and restore of both services.
e2e-ops: build
	test/e2e/ops.sh

# HTTPS through ACME (Let's Encrypt protocol) against Pebble in Docker.
e2e-acme: build
	test/e2e/acme.sh

# Windows installers of both servers (needs NSIS: apt install nsis) → dist/windows/
windows:
	packaging/windows/build.sh $(VERSION)

# --- desktop client (Electron + web UI, in client/) ---

client-install:
	cd client && npm ci

# Desktop app in development mode (UI reloads on change). Start an Identity service first (make run-identity)
# and choose "localhost:8080" under "Service d'identité" on the sign-in screen.
client-dev:
	cd client && [ -d node_modules ] || npm ci
	cd client && npm run dev

client-build:
	cd client && npm run build

# Unit tests and type checks of the client.
client-test:
	cd client && npx tsc --noEmit && npx vitest run

# End-to-end: the real desktop app (virtual display) against a real Identity service.
e2e-client: build
	cd client && [ -d node_modules ] || npm ci
	cd client && npx vite build && node scripts/build-electron.mjs && xvfb-run -a npx playwright test
