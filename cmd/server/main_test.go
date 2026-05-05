package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClaudeEnvConfig_MissingToken(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	_, err := loadClaudeEnvConfig()
	if err == nil {
		t.Fatal("expected error when ANTHROPIC_AUTH_TOKEN is empty")
	}
}

func TestLoadClaudeEnvConfig_TokenOnly(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-token")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("API_TIMEOUT_MS", "")
	t.Setenv("CLAUDE_MODEL", "")

	cfg, err := loadClaudeEnvConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthToken != "test-token" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "test-token")
	}
	if cfg.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty", cfg.BaseURL)
	}
	if cfg.Timeout != "" {
		t.Errorf("Timeout = %q, want empty", cfg.Timeout)
	}
	if cfg.Model != "" {
		t.Errorf("Model = %q, want empty", cfg.Model)
	}
}

func TestLoadClaudeEnvConfig_FullConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "my-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://custom.api.com")
	t.Setenv("API_TIMEOUT_MS", "30000")
	t.Setenv("CLAUDE_MODEL", "opus")
	t.Setenv("ANTHROPIC_DEFAULT_HAIKU_MODEL", "haiku-42")
	t.Setenv("ANTHROPIC_DEFAULT_SONNET_MODEL", "sonnet-42")
	t.Setenv("ANTHROPIC_DEFAULT_OPUS_MODEL", "opus-42")
	t.Setenv("CLAUDE_CODE_DISABLE_1M_CONTEXT", "1")
	t.Setenv("DISABLE_TELEMETRY", "true")
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")

	cfg, err := loadClaudeEnvConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		field string
		want  string
	}{
		{"AuthToken", "my-token"},
		{"BaseURL", "https://custom.api.com"},
		{"Timeout", "30000"},
		{"Model", "opus"},
		{"HaikuModel", "haiku-42"},
		{"SonnetModel", "sonnet-42"},
		{"OpusModel", "opus-42"},
		{"Disable1M", "1"},
		{"DisableTelemetry", "true"},
		{"DisableNonEssential", "1"},
	}
	for _, tt := range tests {
		var got string
		switch tt.field {
		case "AuthToken":
			got = cfg.AuthToken
		case "BaseURL":
			got = cfg.BaseURL
		case "Timeout":
			got = cfg.Timeout
		case "Model":
			got = cfg.Model
		case "HaikuModel":
			got = cfg.HaikuModel
		case "SonnetModel":
			got = cfg.SonnetModel
		case "OpusModel":
			got = cfg.OpusModel
		case "Disable1M":
			got = cfg.Disable1M
		case "DisableTelemetry":
			got = cfg.DisableTelemetry
		case "DisableNonEssential":
			got = cfg.DisableNonEssential
		}
		if got != tt.want {
			t.Errorf("cfg.%s = %q, want %q", tt.field, got, tt.want)
		}
	}
}

func TestConfigureClaudeMCP_EmptyPAT(t *testing.T) {
	t.Setenv("GITHUB_PAT", "")

	result := configureClaudeMCP(t.TempDir())
	if result != "" {
		t.Errorf("expected empty path when GITHUB_PAT is empty, got %q", result)
	}
}

func TestConfigureClaudeMCP_ValidPAT(t *testing.T) {
	t.Setenv("GITHUB_PAT", "ghp_testpat123")

	dir := t.TempDir()
	outPath := filepath.Join(dir, "mcp-config.json")

	result := configureClaudeMCP(outPath)
	if result != outPath {
		t.Errorf("result = %q, want %q", result, outPath)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("missing mcpServers key")
	}

	gh, ok := servers["github"].(map[string]any)
	if !ok {
		t.Fatal("missing github server config")
	}

	if gh["type"] != "http" {
		t.Errorf("github type = %v, want %q", gh["type"], "http")
	}
	if gh["url"] != "https://api.githubcopilot.com/mcp" {
		t.Errorf("github url = %v, want %q", gh["url"], "https://api.githubcopilot.com/mcp")
	}

	headers, ok := gh["headers"].(map[string]any)
	if !ok {
		t.Fatal("missing headers")
	}
	if headers["Authorization"] != "Bearer ghp_testpat123" {
		t.Errorf("Authorization = %v, want %q", headers["Authorization"], "Bearer ghp_testpat123")
	}
}

func TestResolveMCPConfigPath_Default(t *testing.T) {
	t.Setenv("NANO_DATA_DIR", "")
	got := resolveMCPConfigPath()
	want, _ := filepath.Abs(filepath.Join("data", "mcp-config.json"))
	if got != want {
		t.Errorf("resolveMCPConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveMCPConfigPath_CustomDir(t *testing.T) {
	t.Setenv("NANO_DATA_DIR", "/tmp/data")
	got := resolveMCPConfigPath()
	want := filepath.Join("/tmp/data", "mcp-config.json")
	if got != want {
		t.Errorf("resolveMCPConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveLogPath_Default(t *testing.T) {
	t.Setenv("NANO_LOG_DIR", "")
	got := resolveLogPath("review.log")
	want := filepath.Join("logs", "review.log")
	if got != want {
		t.Errorf("resolveLogPath() = %q, want %q", got, want)
	}
}

func TestResolveLogPath_CustomDir(t *testing.T) {
	t.Setenv("NANO_LOG_DIR", "/tmp/logs")
	got := resolveLogPath("review.log")
	want := filepath.Join("/tmp/logs", "review.log")
	if got != want {
		t.Errorf("resolveLogPath() = %q, want %q", got, want)
	}
}

func TestResolveReviewOutputDir_Default(t *testing.T) {
	t.Setenv("NANO_LOG_DIR", "")
	got := resolveReviewOutputDir()
	want := filepath.Join("logs", "reviews")
	if got != want {
		t.Errorf("resolveReviewOutputDir() = %q, want %q", got, want)
	}
}

func TestResolveReviewOutputDir_CustomDir(t *testing.T) {
	t.Setenv("NANO_LOG_DIR", "/tmp/logs")
	got := resolveReviewOutputDir()
	want := filepath.Join("/tmp/logs", "reviews")
	if got != want {
		t.Errorf("resolveReviewOutputDir() = %q, want %q", got, want)
	}
}
