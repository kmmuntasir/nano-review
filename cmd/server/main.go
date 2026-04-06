package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/kmmuntasir/nano-review/internal/api"
	"github.com/kmmuntasir/nano-review/internal/reviewer"
)

// claudeCLI implements reviewer.ClaudeRunner by executing the Claude Code binary.
type claudeCLI struct{}

func (c *claudeCLI) Run(ctx context.Context, dir string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir

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

func main() {
	requiredEnvs := []string{"WEBHOOK_SECRET", "ANTHROPIC_API_KEY", "GITHUB_PAT"}
	for _, env := range requiredEnvs {
		if os.Getenv(env) == "" {
			slog.Error("required environment variable not set", "variable", env)
			os.Exit(1)
		}
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

	worker := reviewer.NewWorker(&claudeCLI{}, logger, "git", claudePath)

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
