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

# Install Claude Code CLI
RUN curl -fsSL https://claude.ai/install.sh | sh

# Copy binary from builder
COPY --from=builder /nano-review /usr/local/bin/nano-review

# Copy Claude Code configuration
COPY config/.claude/ /root/.claude/

# Create log directory
RUN mkdir -p /app/logs

WORKDIR /app

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/nano-review"]
