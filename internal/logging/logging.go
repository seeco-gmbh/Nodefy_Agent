package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func SetupFileLogging() (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	logDir := filepath.Join(home, ".nodefy")
	logPath := filepath.Join(logDir, "agent.log")

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	if info, err := os.Stat(logPath); err == nil && info.Size() > 10*1024*1024 {
		oldPath := logPath + ".old"
		// Best-effort rotation: ignore errors — old backup may not exist, and
		// a failed rename just means the log file continues to grow.
		_ = os.Remove(oldPath)
		_ = os.Rename(logPath, oldPath)
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	if _, err := fmt.Fprintf(file, "\n=== Nodefy Agent started at %s ===\n", time.Now().Format(time.RFC3339)); err != nil {
		log.Warn().Err(err).Msg("Failed to write log header")
	}

	multi := io.MultiWriter(file, zerolog.ConsoleWriter{Out: os.Stderr})
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()

	return file, nil
}

func RecoverWithDialog() {
	if r := recover(); r != nil {
		errMsg := fmt.Sprintf("Nodefy Agent crashed: %v", r)

		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		stack := string(buf[:n])

		log.Error().
			Str("panic", fmt.Sprintf("%v", r)).
			Str("stack", stack).
			Msg("Agent crashed")

		if runtime.GOOS == "windows" {
			showWindowsErrorDialog(errMsg)
		} else {
			// Writing to stderr on fatal crash — error is intentionally ignored.
			_, _ = fmt.Fprintf(os.Stderr, "FATAL: %s\n%s\n", errMsg, stack)
		}

		os.Exit(1)
	}
}
