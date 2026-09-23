#!/usr/bin/env bash
# QUAREL_TLS=acme end to end, against Pebble (Let's Encrypt's test ACME server)
# with its test DNS server resolving quarel.test to this machine. Needs Docker.
# Run from the repo: make e2e-acme
set -uo pipefail
export PATH="$HOME/.local/go/bin:$PATH"
REPO=$(cd "$(dirname "$0")/../.." && pwd); B=$REPO/bin
D=$(mktemp -d); SRV=; SHIM=
trap 'kill $SRV $SHIM 2>/dev/null; [ -n "${KEEP:-}" ] || docker rm -f quarel-pebble quarel-challtestsrv >/dev/null 2>&1; rm -rf "$D"' EXIT
PORT=18443
cat > $D/pebble.json <<JSON
{"pebble": {"listenAddress": "0.0.0.0:14000", "managementListenAddress": "0.0.0.0:15000",
  "certificate": "test/certs/localhost/cert.pem", "privateKey": "test/certs/localhost/key.pem",
  "httpPort": 5002, "tlsPort": $PORT, "ocspResponderURL": "", "externalAccountBindingRequired": false}}
JSON
docker run -d --name quarel-challtestsrv --network host ghcr.io/letsencrypt/pebble-challtestsrv:latest \
  -defaultIPv4 127.0.0.1 -defaultIPv6 "" -dnsserver :18053 -http01 "" -https01 "" -tlsalpn01 "" -doh "" -management :18055 >/dev/null
docker run -d --name quarel-pebble --network host -e PEBBLE_VA_NOSLEEP=1 -v $D/pebble.json:/config.json:ro \
  ghcr.io/letsencrypt/pebble:latest -config /config.json -dnsserver 127.0.0.1:18053 >/dev/null
sleep 1.5
docker cp quarel-pebble:/test/certs/pebble.minica.pem $D/pebble-api-ca.pem >/dev/null
docker cp quarel-pebble:/test/certs/localhost/cert.pem $D/shim.crt >/dev/null
docker cp quarel-pebble:/test/certs/localhost/key.pem $D/shim.key >/dev/null
# Pebble omits a header Let's Encrypt sends (see acmeshim): go through the shim.
go build -o $D/acmeshim "$REPO/test/e2e/acmeshim" || exit 1
$D/acmeshim localhost:14443 https://localhost:14000 $D/shim.crt $D/shim.key $D/pebble-api-ca.pem > $D/shim.log 2>&1 &
SHIM=$!; sleep 0.5
QUAREL_ADDR=127.0.0.1:$PORT QUAREL_TLS=acme QUAREL_TLS_DOMAIN=quarel.test QUAREL_ACME_DIRECTORY=https://localhost:14443/dir \
  QUAREL_ACME_CA_FILE=$D/pebble-api-ca.pem QUAREL_DATA_DIR=$D/srv QUAREL_UPNP=off QUAREL_VOICE=off $B/quarel-server > $D/srv.log 2>&1 &
SRV=$!; sleep 1
# The first TLS handshake for quarel.test triggers issuance (TLS-ALPN-01 answered on the same port).
curl -sk -m 60 --resolve quarel.test:$PORT:127.0.0.1 https://quarel.test:$PORT/v1/health >/dev/null
sleep 1
curl -sk -m 10 https://localhost:15000/roots/0 > $D/pebble-root.pem
OUT=$(curl -s -m 20 --cacert $D/pebble-root.pem --resolve quarel.test:$PORT:127.0.0.1 https://quarel.test:$PORT/v1/server -w ' HTTP %{http_code}')
ISSUER=$(echo | timeout 10 openssl s_client -connect 127.0.0.1:$PORT -servername quarel.test 2>/dev/null | openssl x509 -noout -issuer 2>/dev/null)
if [[ "$OUT" == *'"access"'*"HTTP 200"* && "$ISSUER" == *Pebble* ]]; then
  echo "✔ certificat obtenu par ACME (émetteur : ${ISSUER#issuer=}) et reconnu par un client standard"
  [ -n "$(ls $D/srv/acme 2>/dev/null)" ] && echo "✔ certificat mis en cache dans data/acme (pas de nouvelle demande au redémarrage)"
else
  echo "✘ échec ACME : $OUT / $ISSUER"; tail -20 $D/srv.log; docker logs quarel-pebble 2>&1 | tail -20; exit 1
fi
