# Stage 1: Build both binaries
FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server/
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/crawler ./cmd/crawler/
# subfinder is shelled out to at runtime (internal/subfinder), not imported —
# installed standalone so it's pinned independent of this module's go.mod.
RUN CGO_ENABLED=0 GOOS=linux go install github.com/projectdiscovery/subfinder/v2/cmd/subfinder@latest && \
    mv "$(go env GOPATH)/bin/subfinder" /out/subfinder

# Stage 1b: build the frontend — separate builder since golang:bookworm has no Node.
# API calls are same-origin relative paths (web/src/api/client.ts), so the
# build is domain-agnostic: one image serves staging or prod, no build args.
FROM node:22-alpine AS web-builder
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2a: server runtime — no Chrome, needs subfinder
FROM debian:bookworm-slim AS server
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/server    /app/server
COPY --from=builder /out/subfinder /app/subfinder
COPY dns-server.yaml               /app/dns-server.yaml

WORKDIR /app
ENV SUBFINDER_PATH=/app/subfinder

EXPOSE 8080 50051
ENTRYPOINT ["/app/server", "--seed-dns", "/app/dns-server.yaml"]

# Stage 2b: crawler runtime — needs Chrome for screenshots, no subfinder.
# chromedp/headless-shell ships a purpose-built headless Chrome binary
# (no full browser UI/sandbox extras), far smaller than apt's chromium
# package; chromedp auto-detects it by name on PATH (see allocate.go).
FROM chromedp/headless-shell:latest AS crawler
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/crawler /app/crawler

WORKDIR /app
EXPOSE 50052
ENTRYPOINT ["/app/crawler"]

# Stage 2c: web runtime — static frontend only, no Go/Node in the final image
FROM nginx:alpine AS web
COPY web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=web-builder /src/web/dist /usr/share/nginx/html
EXPOSE 80
