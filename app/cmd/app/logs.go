package main

import (
	"log/slog"
	"os"
	"path/filepath"
)

// makeLogsPrivate lets only the current user read HardLlama's logs,
// including ones written by earlier versions with wider permissions.
func makeLogsPrivate(logPath string) {
	dir := filepath.Dir(logPath)
	if err := os.Chmod(dir, 0o700); err != nil {
		slog.Warn("failed to make log folder private", "error", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "hardllama-*.log"))
	if err != nil {
		return
	}
	for _, file := range files {
		if err := os.Chmod(file, 0o600); err != nil {
			slog.Warn("failed to make log private", "error", err)
		}
	}
}
