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
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user (Claude Code refuses --dangerously-skip-permissions as root)
RUN useradd -m -s /bin/bash appuser

# Create SSH directory with proper permissions for appuser
RUN mkdir -p /home/appuser/.ssh && \
    chown appuser:appuser /home/appuser/.ssh && \
    chmod 700 /home/appuser/.ssh

# Copy SSH configuration
COPY config/.ssh/config /home/appuser/.ssh/config
RUN chown appuser:appuser /home/appuser/.ssh/config && \
    chmod 600 /home/appuser/.ssh/config

# Copy SSH deploy key (must exist locally - see keys/README.md)
# This will fail if keys/deploy_key doesn't exist, which is intentional
COPY keys/deploy_key /home/appuser/.ssh/deploy_key
RUN chown appuser:appuser /home/appuser/.ssh/deploy_key && \
    chmod 600 /home/appuser/.ssh/deploy_key && \
    echo "SSH deploy key installed successfully"

# Pre-populate known_hosts for github.com so git clone doesn't prompt
RUN ssh-keyscan github.com > /home/appuser/.ssh/known_hosts && \
    chown appuser:appuser /home/appuser/.ssh/known_hosts && \
    chmod 644 /home/appuser/.ssh/known_hosts

# Install Claude Code CLI as appuser
USER appuser
RUN curl -fsSL https://claude.ai/install.sh | bash

# Copy binary from builder
COPY --from=builder --chown=appuser:appuser /nano-review /usr/local/bin/nano-review

# Copy Claude Code configuration
COPY --chown=appuser:appuser config/.claude/ /home/appuser/.claude/

# Create log directories (needs root to create /app)
USER root
RUN mkdir -p /app/logs/reviews && \
    chown -R appuser:appuser /app

WORKDIR /app

USER appuser

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/nano-review"]
