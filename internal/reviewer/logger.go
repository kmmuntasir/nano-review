package reviewer

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// slogLogger implements Logger by wrapping multiple slog.Logger instances.
// It dispatches Info/Error/With calls to all underlying loggers, enabling
// simultaneous output to stdout (text) and file (JSON with rotation).
type slogLogger struct {
	loggers []*slog.Logger
}

// NewLogger creates a Logger that writes to both stdout (text format) and a
// JSON log file at the given path with lumberjack rotation.
//
// Lumberjack config: 10MB max size, 7 days max age, 3 backups, compressed.
func NewLogger(filePath string) (Logger, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, err
	}

	fileWriter := &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    10, // megabytes
		MaxBackups: 3,
		MaxAge:     7, // days
		Compress:   true,
	}

	stdoutHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	return &slogLogger{
		loggers: []*slog.Logger{
			slog.New(stdoutHandler),
			slog.New(fileHandler),
		},
	}, nil
}

// newNopLogger returns a no-op Logger that discards all output. Useful for tests.
func newNopLogger() Logger {
	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})
	return &slogLogger{
		loggers: []*slog.Logger{slog.New(handler)},
	}
}

// Info logs a message at Info level to all underlying loggers.
func (l *slogLogger) Info(msg string, keysAndValues ...any) {
	for _, lg := range l.loggers {
		lg.Info(msg, keysAndValues...)
	}
}

// Error logs a message at Error level to all underlying loggers.
func (l *slogLogger) Error(msg string, keysAndValues ...any) {
	for _, lg := range l.loggers {
		lg.Error(msg, keysAndValues...)
	}
}

// With returns a new Logger with the given key-value pairs added to all
// underlying loggers.
func (l *slogLogger) With(keysAndValues ...any) Logger {
	enriched := make([]*slog.Logger, len(l.loggers))
	for i, lg := range l.loggers {
		enriched[i] = lg.With(keysAndValues...)
	}
	return &slogLogger{loggers: enriched}
}
