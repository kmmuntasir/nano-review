package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/reviewer"
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
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir

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

	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return string(out), 1, err
		}
	}

	return string(out), exitCode, nil
}

// configureClaudeMCP registers the GitHub MCP server with Claude Code via
// `claude mcp add`. This is required because Claude Code v2.1+ ignores
// mcpServers in settings.json and reads from .claude.json instead.
func configureClaudeMCP(claudePath string) {
	pat := os.Getenv("GITHUB_PAT")
	if pat == "" {
		slog.Error("GITHUB_PAT not set, skipping MCP configuration")
		return
	}

	cmd := exec.Command(claudePath, "mcp", "add", "github",
		"-t", "http",
		"-s", "user",
		"-H", fmt.Sprintf("Authorization: Bearer %s", pat),
		"--", "https://api.githubcopilot.com/mcp",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("failed to configure GitHub MCP server", "error", err, "output", string(out))
		return
	}
	slog.Info("GitHub MCP server configured successfully", "output", strings.TrimSpace(string(out)))
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
	slog.Info("loaded configuration", "claude_path", claudePath, "model", model, "base_url", claudeConfig.BaseURL)

	worker := reviewer.NewWorker(&claudeCLI{env: claudeConfig}, logger, "git", claudePath, model)

	configureClaudeMCP(claudePath)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /review", api.HandleReview(webhookSecret, worker))

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
