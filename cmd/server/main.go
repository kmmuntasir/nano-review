package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/reviewer"
	"github.com/kmmuntasir/nano-review/internal/storage"
	"github.com/kmmuntasir/nano-review/web"
)

// claudeEnvConfig holds environment variables passed through to the Claude Code CLI.
type claudeEnvConfig struct {
	AuthToken           string
	BaseURL             string
	Timeout             string
	Model               string
	HaikuModel          string
	SonnetModel         string
	OpusModel           string
	Disable1M           string
	DisableTelemetry    string
	DisableNonEssential string
}

func loadClaudeEnvConfig() (*claudeEnvConfig, error) {
	authToken := os.Getenv("ANTHROPIC_AUTH_TOKEN")
	if authToken == "" {
		return nil, fmt.Errorf("ANTHROPIC_AUTH_TOKEN is required")
	}
	return &claudeEnvConfig{
		AuthToken:           authToken,
		BaseURL:             os.Getenv("ANTHROPIC_BASE_URL"),
		Timeout:             os.Getenv("API_TIMEOUT_MS"),
		Model:               os.Getenv("CLAUDE_MODEL"),
		HaikuModel:          os.Getenv("ANTHROPIC_DEFAULT_HAIKU_MODEL"),
		SonnetModel:         os.Getenv("ANTHROPIC_DEFAULT_SONNET_MODEL"),
		OpusModel:           os.Getenv("ANTHROPIC_DEFAULT_OPUS_MODEL"),
		Disable1M:           os.Getenv("CLAUDE_CODE_DISABLE_1M_CONTEXT"),
		DisableTelemetry:    os.Getenv("DISABLE_TELEMETRY"),
		DisableNonEssential: os.Getenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"),
	}, nil
}

// claudeCLI implements reviewer.ClaudeRunner by executing the Claude Code binary.
type claudeCLI struct {
	env *claudeEnvConfig
}

func (c *claudeCLI) Run(ctx context.Context, dir string, args ...string) (string, int, error) {
	var buf bytes.Buffer
	exitCode, err := c.RunStreaming(ctx, dir, &buf, args...)
	return buf.String(), exitCode, err
}

func (c *claudeCLI) RunStreaming(ctx context.Context, dir string, streamWriter io.Writer, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Build child process environment: inherit parent + inject Claude Code vars
	env := os.Environ()
	env = append(env, "ANTHROPIC_AUTH_TOKEN="+c.env.AuthToken)
	if c.env.BaseURL != "" {
		env = append(env, "ANTHROPIC_BASE_URL="+c.env.BaseURL)
	}
	if c.env.Timeout != "" {
		env = append(env, "API_TIMEOUT_MS="+c.env.Timeout)
	}
	if c.env.HaikuModel != "" {
		env = append(env, "ANTHROPIC_DEFAULT_HAIKU_MODEL="+c.env.HaikuModel)
	}
	if c.env.SonnetModel != "" {
		env = append(env, "ANTHROPIC_DEFAULT_SONNET_MODEL="+c.env.SonnetModel)
	}
	if c.env.OpusModel != "" {
		env = append(env, "ANTHROPIC_DEFAULT_OPUS_MODEL="+c.env.OpusModel)
	}
	if c.env.Disable1M != "" {
		env = append(env, "CLAUDE_CODE_DISABLE_1M_CONTEXT="+c.env.Disable1M)
	}
	if c.env.DisableTelemetry != "" {
		env = append(env, "DISABLE_TELEMETRY="+c.env.DisableTelemetry)
	}
	if c.env.DisableNonEssential != "" {
		env = append(env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC="+c.env.DisableNonEssential)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 1, fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("start claude process: %w", err)
	}

	// Drain stdout line-by-line and write to streamWriter.
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 512*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		streamWriter.Write([]byte(line + "\n"))
	}

	// Drain stderr for diagnostics.
	stderrBytes, _ := io.ReadAll(stderr)

	// Kill the entire process group to clean up any child processes.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	waitErr := cmd.Wait()

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return 1, waitErr
		}
	}

	if len(stderrBytes) > 0 {
		slog.Error("claude stderr output", "output", string(stderrBytes))
	}

	return exitCode, nil
}

// claudeMCPConfigPath is the path to the MCP config file passed to Claude Code
// via --mcp-config. Using a dedicated file with --strict-mcp-config prevents
// project-level .mcp.json files in cloned repos from overriding our GitHub MCP.
const claudeMCPConfigPath = "/app/mcp-config.json"

// configureClaudeMCP writes a dedicated MCP config file with the GitHub Copilot
// MCP server using GITHUB_PAT for authentication. This file is passed to Claude
// Code via --mcp-config --strict-mcp-config to prevent project-level .mcp.json
// files in cloned repos from interfering with the review workflow.
func configureClaudeMCP() string {
	pat := os.Getenv("GITHUB_PAT")
	if pat == "" {
		slog.Error("GITHUB_PAT not set, skipping MCP configuration")
		return ""
	}

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"github": map[string]any{
				"type": "http",
				"url":  "https://api.githubcopilot.com/mcp",
				"headers": map[string]string{
					"Authorization": "Bearer " + pat,
				},
			},
		},
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		slog.Error("failed to marshal MCP config", "error", err)
		return ""
	}

	if err := os.WriteFile(claudeMCPConfigPath, out, 0644); err != nil {
		slog.Error("failed to write MCP config file", "path", claudeMCPConfigPath, "error", err)
		return ""
	}
	slog.Info("GitHub MCP server configured", "config_path", claudeMCPConfigPath)
	return claudeMCPConfigPath
}

func main() {
	for _, env := range []string{"WEBHOOK_SECRET", "GITHUB_PAT"} {
		if os.Getenv(env) == "" {
			slog.Error("required environment variable not set", "variable", env)
			os.Exit(1)
		}
	}

	claudeConfig, err := loadClaudeEnvConfig()
	if err != nil {
		slog.Error("failed to load Claude env config", "error", err)
		os.Exit(1)
	}

	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	githubPat := os.Getenv("GITHUB_PAT")
	claudePath := os.Getenv("CLAUDE_CODE_PATH")
	if claudePath == "" {
		claudePath = "claude"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger, err := reviewer.NewLogger("/app/logs/review.log")
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		os.Exit(1)
	}

	model := os.Getenv("CLAUDE_MODEL")

	maxReviewDuration := reviewer.DefaultMaxReviewDuration
	if v := os.Getenv("MAX_REVIEW_DURATION"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			maxReviewDuration = time.Duration(secs) * time.Second
		} else {
			slog.Error("invalid MAX_REVIEW_DURATION, using default", "value", v, "error", err)
		}
	}

	slog.Info("loaded configuration", "claude_path", claudePath, "model", model, "base_url", claudeConfig.BaseURL, "max_review_duration", maxReviewDuration)

	maxRetries := reviewer.DefaultMaxRetries
	if v := os.Getenv("MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n < 0 {
				n = 0
			}
			maxRetries = n
		} else {
			slog.Error("invalid MAX_RETRIES, using default", "value", v, "error", err)
		}
	}

	mcpConfigPath := configureClaudeMCP()

	dbPath := os.Getenv("DATABASE_PATH")
	store, err := storage.Open(dbPath)
	if err != nil {
		slog.Error("failed to initialize database", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer store.Close()
	slog.Info("database initialized", "path", dbPath)

	hub := api.NewHub()

	worker := reviewer.NewWorker(&claudeCLI{env: claudeConfig}, store, logger, hub, "git", claudePath, model, mcpConfigPath, githubPat, maxReviewDuration, maxRetries)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /review", api.HandleReview(webhookSecret, worker))
	mux.HandleFunc("GET /reviews", api.HandleListReviews(store))
	mux.HandleFunc("GET /reviews/{run_id}", api.HandleGetReview(store))
	mux.HandleFunc("GET /ws", api.HandleWebSocket(hub))
	mux.HandleFunc("GET /metrics", api.HandleGetMetrics(store))

	mux.Handle("GET /", http.StripPrefix("/", http.FileServer(http.FS(web.FS))))

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown failed", "error", err)
	}
}
