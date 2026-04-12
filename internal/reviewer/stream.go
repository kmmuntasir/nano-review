package reviewer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kmmuntasir/nano-review/internal/api"
)

// streamAccumulator is an io.Writer that writes streaming JSON lines to a file
// on disk (flushed immediately) while accumulating the raw bytes in memory
// for later use in the .txt output file and database storage.
type streamAccumulator struct {
	file *os.File
	buf  bytes.Buffer
}

func newStreamAccumulator(path string) (*streamAccumulator, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create stream output directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create stream file %s: %w", path, err)
	}
	return &streamAccumulator{file: f}, nil
}

func (a *streamAccumulator) Write(p []byte) (int, error) {
	n, err := a.file.Write(p)
	if err != nil {
		return n, err
	}
	a.buf.Write(p)
	return n, a.file.Sync()
}

func (a *streamAccumulator) Close() error {
	return a.file.Close()
}

// Text returns the accumulated raw bytes as a string.
func (a *streamAccumulator) Text() string {
	return a.buf.String()
}

// wsStreamWriter wraps a streamAccumulator and broadcasts each write to
// WebSocket subscribers on the topic "run:<runID>".
type wsStreamWriter struct {
	accum      *streamAccumulator
	broadcaster Broadcaster
	runID      string
}

func newWSStreamWriter(accum *streamAccumulator, broadcaster Broadcaster, runID string) *wsStreamWriter {
	return &wsStreamWriter{
		accum:       accum,
		broadcaster: broadcaster,
		runID:       runID,
	}
}

func (w *wsStreamWriter) Write(p []byte) (int, error) {
	n, err := w.accum.Write(p)
	if err != nil {
		return n, err
	}
	if w.broadcaster != nil && len(p) > 0 {
		msg, _ := json.Marshal(map[string]string{
			"type":   "stream",
			"run_id": w.runID,
			"data":   string(p),
		})
		w.broadcaster.Broadcast("run:"+w.runID, msg)
	}
	return n, nil
}

func (w *wsStreamWriter) Close() error {
	return w.accum.Close()
}

func (w *wsStreamWriter) Text() string {
	return w.accum.Text()
}

// streamFilePath computes the path for the .stream.json file using the same
// naming convention as saveReviewOutput.
func streamFilePath(runID string, p api.ReviewPayload) string {
	repoSlug := p.RepoURL
	if idx := strings.LastIndex(repoSlug, "/"); idx >= 0 {
		repoSlug = repoSlug[idx+1:]
	}
	repoSlug = strings.TrimSuffix(repoSlug, ".git")

	ts := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s_%s_pr%d_%s.stream.json", ts, repoSlug, p.PRNumber, runID[:8])
	return filepath.Join(reviewOutputDir, filename)
}
