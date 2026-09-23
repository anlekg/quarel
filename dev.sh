#!/usr/bin/env bash
# Dev helper: ./dev.sh <build|test|vet|run-identity|docker-identity|clean>
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.local/go/bin:$PATH"

case "${1:-}" in
  build)           go build -o bin/ ./cmd/... ;;
  test)            go test ./... ;;
  vet)             go vet ./... ;;
  # Local dev server: data in ./data, verification codes printed in the log.
  run-identity)    go build -o bin/ ./cmd/... && QUAREL_ISSUER=localhost:8080 exec ./bin/quarel-identity ;;
  docker-identity) docker build -f Dockerfile.identity -t quarel-identity . ;;
  clean)           rm -rf bin ;;
  *) echo "usage: $0 <build|test|vet|run-identity|docker-identity|clean>" >&2; exit 2 ;;
esac
