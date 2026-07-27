#!/usr/bin/env bash
set -euo pipefail
# Job control: gives each backgrounded job (server, frontend) its own process
# group, so cleanup's group-kill also reaches children go run/npm spawn under
# them (go run's compiled binary, npm's vite/node) instead of orphaning them.
set -m

DB_URL="host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
CRAWLER_TOKEN="dev-secret"
SERVER_PID=""
VITE_PID=""
CRAWLER_PID=""

cleanup() {
  echo ""
  echo "Shutting down..."
  [[ -n "$SERVER_PID" ]]  && { kill -- -"$SERVER_PID"  2>/dev/null || kill "$SERVER_PID"  2>/dev/null || true; }
  [[ -n "$VITE_PID" ]]    && { kill -- -"$VITE_PID"    2>/dev/null || kill "$VITE_PID"    2>/dev/null || true; }
  [[ -n "$CRAWLER_PID" ]] && { kill -- -"$CRAWLER_PID" 2>/dev/null || kill "$CRAWLER_PID" 2>/dev/null || true; }
  docker compose -f docker-compose.yml -f docker-compose.dev.yml stop postgres minio
}
trap cleanup EXIT INT TERM

echo "==> Building crawler..."
go build -o crawler ./cmd/crawler/

echo "==> Starting PostgreSQL..."
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d postgres
echo -n "    Waiting for postgres"
until docker compose -f docker-compose.yml -f docker-compose.dev.yml exec -T postgres pg_isready -U postgres -q 2>/dev/null; do
  echo -n "."
  sleep 1
done
echo " ready"

echo "==> Starting MinIO..."
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d minio minio-init
echo -n "    Waiting for MinIO"
until curl -sf -o /dev/null http://localhost:9000/minio/health/live 2>/dev/null; do
  echo -n "."
  sleep 1
done
echo " ready"

echo "==> Starting crawler control service on :50052..."
# Host must be explicit, not a bare port: under mTLS the client verifies this
# name against the certificate's SANs, and ":50051" supplies no name at all.
./crawler --listen-addr :50052 --grpc-addr localhost:50051 --auth-token "$CRAWLER_TOKEN" > >(sed -u 's/^/[crawler] /') 2>&1 &
CRAWLER_PID=$!

echo "==> Starting server on :8080..."
go run ./cmd/server/ \
  --db-url "$DB_URL" \
  --http-addr :8080 \
  --grpc-addr :50051 \
  --crawler-addr localhost:50052 \
  --crawler-token "$CRAWLER_TOKEN" \
  --subfinder-path "$(go env GOPATH)/bin/subfinder" \
  --cookie-secure=false \
  --bootstrap-admin-username admin \
  --bootstrap-admin-password admin \
  --seed-dns dns-server.yaml > >(sed -u 's/^/[server] /') 2>&1 &
SERVER_PID=$!

echo "==> Starting frontend on :5173..."
npm --prefix web run dev > >(sed -u 's/^/[web] /') 2>&1 &
VITE_PID=$!

echo ""
echo "Dev stack running:"
echo "  Frontend  http://localhost:5173"
echo "  API       http://localhost:8080"
echo ""
echo "Press Ctrl+C to stop."

wait "$SERVER_PID" "$VITE_PID" "$CRAWLER_PID"
