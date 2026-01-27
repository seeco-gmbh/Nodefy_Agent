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

// SetupFileLogging creates a log file at ~/.nodefy/agent.log
// Returns the file handle (caller should defer Close) and any error
func SetupFileLogging() (*os.File, error) {
	// Get config directory
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	logDir := filepath.Join(home, ".nodefy")
	logPath := filepath.Join(logDir, "agent.log")

	// Ensure directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Rotate log if it's too large (> 10MB)
	if info, err := os.Stat(logPath); err == nil && info.Size() > 10*1024*1024 {
		oldPath := logPath + ".old"
		os.Remove(oldPath)
		os.Rename(logPath, oldPath)
	}

	// Open log file (append mode)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Write startup marker
	fmt.Fprintf(file, "\n=== Nodefy Agent started at %s ===\n", time.Now().Format(time.RFC3339))

	// Setup multi-writer (file + console)
	multi := io.MultiWriter(file, zerolog.ConsoleWriter{Out: os.Stderr})
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()

	return file, nil
}

// RecoverWithDialog recovers from panics and shows an error dialog on Windows
// On other platforms, it logs the error and exits
func RecoverWithDialog() {
	if r := recover(); r != nil {
		errMsg := fmt.Sprintf("Nodefy Agent crashed: %v", r)

		// Get stack trace
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		stack := string(buf[:n])

		// Log the error
		log.Error().
			Str("panic", fmt.Sprintf("%v", r)).
			Str("stack", stack).
			Msg("Agent crashed")

		// Show dialog on Windows
		if runtime.GOOS == "windows" {
			showWindowsErrorDialog(errMsg)
		} else {
			fmt.Fprintf(os.Stderr, "FATAL: %s\n%s\n", errMsg, stack)
		}

		os.Exit(1)
	}
}

// GetLogPath returns the path to the log file
func GetLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".nodefy", "agent.log")
}
