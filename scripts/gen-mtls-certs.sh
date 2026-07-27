#!/usr/bin/env bash
set -euo pipefail

# Generates a private CA plus one leaf certificate per binary, for the mutual
# TLS gRPC link between cmd/server and cmd/crawler.
#
# This CA is unrelated to any public HTTPS certificate the dashboard's domain
# may use. It is trusted only by these two binaries, it authenticates BOTH
# directions, and it must work for peers that may have no public hostname at
# all — none of which a public CA does.
#
# Each leaf carries serverAuth AND clientAuth, because each binary is both a
# TLS server and a TLS client. A cert with only serverAuth fails the handshake
# with an opaque "bad certificate".
#
# ponytail: 10-year validity, no rotation automation — proportionate for a
# two-process private link. If this grows more peers, replace with
# short-lived certs from something like step-ca or Vault.
#
# Usage:
#   scripts/gen-mtls-certs.sh                      # localhost + docker service names
#   scripts/gen-mtls-certs.sh crawler.internal 10.0.0.7   # plus split-host peers

OUT_DIR="${OUT_DIR:-certs}"
DAYS=3650

# Every address either binary dials must appear here: dev.sh uses localhost,
# docker-compose.yml uses the service names `server` and `crawler`.
SANS=("DNS:localhost" "IP:127.0.0.1" "DNS:server" "DNS:crawler")
for extra in "$@"; do
  if [[ "$extra" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    SANS+=("IP:$extra")
  else
    SANS+=("DNS:$extra")
  fi
done
SAN_LIST=$(IFS=,; echo "${SANS[*]}")

mkdir -p "$OUT_DIR"

echo "==> Generating CA"
openssl req -x509 -newkey rsa:4096 -sha256 -days "$DAYS" -nodes \
  -keyout "$OUT_DIR/ca.key" -out "$OUT_DIR/ca.crt" \
  -subj "/CN=dns-compliance-grpc-ca" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,cRLSign"

for name in server crawler; do
  echo "==> Generating leaf: $name"
  openssl req -newkey rsa:4096 -sha256 -nodes \
    -keyout "$OUT_DIR/$name.key" -out "$OUT_DIR/$name.csr" \
    -subj "/CN=dns-compliance-$name"

  openssl x509 -req -in "$OUT_DIR/$name.csr" -sha256 -days "$DAYS" \
    -CA "$OUT_DIR/ca.crt" -CAkey "$OUT_DIR/ca.key" -CAcreateserial \
    -out "$OUT_DIR/$name.crt" \
    -extfile <(printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth,clientAuth\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\n' "$SAN_LIST")

  rm -f "$OUT_DIR/$name.csr"
done

rm -f "$OUT_DIR/ca.srl"
chmod 600 "$OUT_DIR"/*.key

echo
echo "Wrote to $OUT_DIR/ — SANs: $SAN_LIST"
echo "Server:  --tls-cert $OUT_DIR/server.crt  --tls-key $OUT_DIR/server.key  --tls-ca $OUT_DIR/ca.crt"
echo "Crawler: --tls-cert $OUT_DIR/crawler.crt --tls-key $OUT_DIR/crawler.key --tls-ca $OUT_DIR/ca.crt"
