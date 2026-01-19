package adapter

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SetupLogger initializes the slog logger with file output
func SetupLogger(cfg *LoggingConfig) (*slog.Logger, error) {
	// Expand ~ in path
	logPath := cfg.File
	if strings.HasPrefix(logPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		logPath = filepath.Join(home, logPath[1:])
	}

	// Ensure log directory exists
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Parse log level
	level := parseLogLevel(cfg.Level)

	// Create JSON handler for structured logging
	handler := slog.NewJSONHandler(logFile, &slog.HandlerOptions{
		Level: level,
	})

	logger := slog.New(handler)
	return logger, nil
}

// parseLogLevel converts a string log level to slog.Level
func parseLogLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NullLogger returns a logger that discards all output
func NullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
