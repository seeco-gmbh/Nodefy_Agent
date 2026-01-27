//go:build !windows

package logging

// showWindowsErrorDialog is a no-op on non-Windows platforms
func showWindowsErrorDialog(message string) {
	// No-op on non-Windows platforms
	// Error is already logged to file and stderr
}
