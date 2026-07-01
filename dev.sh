#!/usr/bin/env bash
set -euo pipefail

DB_URL="host=localhost user=postgres password=postgres dbname=dns_compliance port=5432 sslmode=disable"
SERVER_PID=""
VITE_PID=""

cleanup() {
  echo ""
  echo "Shutting down..."
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  [[ -n "$VITE_PID" ]]  && kill "$VITE_PID"  2>/dev/null || true
  docker compose -f docker-compose.yml -f docker-compose.dev.yml stop postgres minio
}
trap cleanup EXIT INT TERM

# Stream a command's stdout+stderr with a [prefix] on each line.
# Usage: stream_prefixed [prefix] cmd args...
stream_prefixed() {
  local prefix="$1"; shift
  "$@" 2>&1 | sed -u "s/^/[$prefix] /"
}

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

echo "==> Starting server on :8080..."
stream_prefixed server go run ./cmd/server/ \
  --db-url "$DB_URL" \
  --http-addr :8080 \
  --grpc-addr :50051 \
  --crawler-path ./crawler \
  --cookie-secure=false \
  --bootstrap-admin-username admin \
  --bootstrap-admin-password admin \
  --seed-dns dns-server.yaml &
SERVER_PID=$!

echo "==> Starting frontend on :5173..."
stream_prefixed web npm --prefix web run dev &
VITE_PID=$!

echo ""
echo "Dev stack running:"
echo "  Frontend  http://localhost:5173"
echo "  API       http://localhost:8080"
echo ""
echo "Press Ctrl+C to stop."

wait "$SERVER_PID" "$VITE_PID"
