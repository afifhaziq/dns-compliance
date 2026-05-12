# Stage 1: Build both binaries
FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server/
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/crawler ./cmd/crawler/

# Stage 2: Runtime with Chrome for screenshot support
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    ca-certificates \
    fonts-liberation \
    libnss3 \
    libatk-bridge2.0-0 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxrandr2 \
    libgbm1 \
    libasound2 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/server  /app/server
COPY --from=builder /out/crawler /app/crawler
COPY dns-server.yaml             /app/dns-server.yaml

WORKDIR /app
ENV CRAWLER_PATH=/app/crawler

EXPOSE 8080 50051
ENTRYPOINT ["/app/server", "--seed-dns", "/app/dns-server.yaml"]
