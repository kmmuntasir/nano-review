# Stage 1: Builder
FROM golang:1.23-bookworm AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /nano-review ./cmd/server

# Stage 2: Runtime
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user (Claude Code refuses --dangerously-skip-permissions as root)
RUN useradd -m -s /bin/bash appuser

# Install Claude Code CLI as appuser
USER appuser
RUN curl -fsSL https://claude.ai/install.sh | bash

# Copy binary from builder
COPY --from=builder --chown=appuser:appuser /nano-review /usr/local/bin/nano-review

# Copy Claude Code configuration
COPY --chown=appuser:appuser config/.claude/ /home/appuser/.claude/

# Create log and data directories (needs root to create /app)
USER root
RUN mkdir -p /app/logs/reviews && \
    mkdir -p /app/data && \
    chown -R appuser:appuser /app

WORKDIR /app

USER appuser

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/nano-review"]
