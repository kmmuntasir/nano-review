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

# Create SSH directory with proper permissions
RUN mkdir -p /root/.ssh && \
    chmod 700 /root/.ssh

# Copy SSH configuration
COPY config/.ssh/config /root/.ssh/config
RUN chmod 600 /root/.ssh/config

# Copy SSH deploy key (must exist locally - see keys/README.md)
# This will fail if keys/deploy_key doesn't exist, which is intentional
COPY keys/deploy_key /root/.ssh/deploy_key
RUN chmod 600 /root/.ssh/deploy_key && \
    echo "SSH deploy key installed successfully"

# Install Claude Code CLI
RUN curl -fsSL https://claude.ai/install.sh | bash

# Copy binary from builder
COPY --from=builder /nano-review /usr/local/bin/nano-review

# Copy Claude Code configuration
COPY config/.claude/ /root/.claude/

# Create log directory
RUN mkdir -p /app/logs

WORKDIR /app

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/nano-review"]
