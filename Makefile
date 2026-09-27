export PATH := $(HOME)/.local/go/bin:$(HOME)/.local/bin:$(PATH)

# Machine-specific settings (publication folders…), not in the repository:
#   QUAREL_UPDATES_DIR = …   (app.quarel.app/updates)
#   QUAREL_DOWNLOADS_DIR = … (quarel.app/telechargements)
-include local.mk
export QUAREL_UPDATES_DIR QUAREL_DOWNLOADS_DIR

.PHONY: build test test-race vet run-identity run-server docker-identity docker-server e2e-voice e2e-dm e2e-security e2e-acme e2e-messages e2e-moderation e2e-accounts e2e-dm-groups e2e-calls e2e-ops windows client-interop client-install client-dev client-build client-test e2e-client windows-release site-dev site-build docs-mcp clean

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
	QUAREL_ISSUER=localhost:8080 QUAREL_UPNP=off QUAREL_DATA_DIR=./data/identity ./bin/quarel-identity

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

# npm 11 does not run Electron's install script: fetch its binary explicitly.
client-install:
	cd client && npm ci && node node_modules/electron/install.js

# Desktop app in development mode (UI reloads on change). Start an Identity service first (make run-identity)
# and choose "localhost:8080" under "Service d'identité" on the sign-in screen.
client-dev:
	cd client && [ -d node_modules ] || npm ci
	cd client && npm run dev

client-build:
	cd client && npm run build

# The client's Olm/Megolm (vodozemac, WebAssembly) against the Go test client's (goolm).
client-interop:
	go build -o bin/olminterop ./test/olminterop
	cd client && node crypto/interop.mjs ../bin/olminterop

# Unit tests and type checks of the client.
client-test:
	cd client && npx tsc --noEmit && npx vitest run

# End-to-end: the real desktop app (virtual display) against a real Identity service.
# Desktop app packages (electron-builder) → client/release/: Linux AppImage + .deb, Windows installer (NSIS, built with Wine).
client-dist:
	cd client && [ -d node_modules ] || npm ci
	cd client && npm run dist

client-dist-win:
	cd client && [ -d node_modules ] || npm ci
	cd client && npm run dist:win

# Windows installers of the servers published to quarel.app/telechargements: make windows-release VERSION=0.2.0
windows-release:
	@[ -n "$(VERSION)" ] || { echo "usage : make windows-release VERSION=x.y.z"; exit 1; }
	packaging/windows/release.sh $(VERSION)

# quarel.app: presentation site and wiki (Astro + Starlight) → site/dist/
site-dev:
	cd site && [ -d node_modules ] || npm ci
	cd site && npx astro dev

site-build:
	cd site && [ -d node_modules ] || npm ci
	cd site && npx astro build

# MCP server of the documentation (the wiki of site/) on http://127.0.0.1:8095/mcp
docs-mcp:
	go run ./cmd/quarel-docs-mcp -docs site/src/content/docs

docker-docs-mcp:
	docker build -f Dockerfile.docs-mcp -t quarel-docs-mcp .

# Signed release of the desktop app published to app.quarel.app/updates: make client-release VERSION=0.2.0
client-release:
	@[ -n "$(VERSION)" ] || { echo "usage : make client-release VERSION=x.y.z"; exit 1; }
	cd client && [ -d node_modules ] || npm ci
	cd client && node scripts/release.mjs $(VERSION)

e2e-web: build     # the web client only, in Chromium (invite links, encrypted storage, installable app, phone layout)
	cd client && [ -d node_modules ] || npm ci
	cd client && npx vite build && npx playwright test --project=web

e2e-client: build
	cd client && [ -d node_modules ] || npm ci
	cd client && npx vite build && node scripts/build-electron.mjs && xvfb-run -a -s '-screen 0 1280x720x24' npx playwright test # 24 bits: screen capture
