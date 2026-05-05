package reviewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kmmuntasir/nano-review/internal/api"
)

func TestNewStreamAccumulator_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.stream.json")
	acc, err := newStreamAccumulator(path)
	if err != nil {
		t.Fatalf("newStreamAccumulator failed: %v", err)
	}
	defer func() { _ = acc.Close() }()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("stream file was not created")
	}
}

func TestStreamAccumulator_WritePersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.stream.json")
	acc, err := newStreamAccumulator(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = acc.Close() }()

	data := `{"type":"message","content":"hello"}
{"type":"result","content":"done"}`
	_, writeErr := acc.Write([]byte(data))
	if writeErr != nil {
		t.Fatalf("Write failed: %v", writeErr)
	}

	raw := acc.Text()
	if raw != data {
		t.Errorf("Text() = %q, want %q", raw, data)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(fileData) != data {
		t.Errorf("file content = %q, want %q", string(fileData), data)
	}
}

func TestStreamAccumulator_MultipleWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.stream.json")
	acc, err := newStreamAccumulator(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = acc.Close() }()

	_, _ = acc.Write([]byte(`{"type":"a"}`))
	_, _ = acc.Write([]byte("\n"))
	_, _ = acc.Write([]byte(`{"type":"b"}`))

	raw := acc.Text()
	want := "{\"type\":\"a\"}\n{\"type\":\"b\"}"
	if raw != want {
		t.Errorf("Text() = %q, want %q", raw, want)
	}
}

func TestStreamFilePath_Format(t *testing.T) {
	const testDir = "logs/reviews"
	path := streamFilePath("12345678-1234-1234-1234-123456789012", api.ReviewPayload{
		RepoURL:  "git@github.com:owner/repo.git",
		PRNumber: 42,
	}, testDir)

	if !strings.HasSuffix(path, ".stream.json") {
		t.Errorf("path should end with .stream.json, got: %s", path)
	}
	if !strings.Contains(path, "repo_pr42_") {
		t.Errorf("path should contain repo name and PR number, got: %s", path)
	}
	if !strings.HasPrefix(path, testDir) {
		t.Errorf("path should start with %s, got: %s", testDir, path)
	}
	// Verify run ID prefix (first 8 chars)
	if !strings.Contains(path, "12345678") {
		t.Errorf("path should contain run ID prefix, got: %s", path)
	}
}

func TestStreamFilePath_HTTPSURL(t *testing.T) {
	path := streamFilePath("12345678-1234-1234-1234-123456789012", api.ReviewPayload{
		RepoURL:  "https://github.com/owner/repo.git",
		PRNumber: 1,
	}, "logs/reviews")

	if !strings.Contains(path, "repo_pr1_") {
		t.Errorf("path should contain repo name and PR number, got: %s", path)
	}
}
