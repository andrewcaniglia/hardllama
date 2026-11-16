package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMakeLogsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]os.FileMode{
		"hardllama-app.log":      0o600,
		"hardllama-app-1.log":    0o600,
		"hardllama-server-2.log": 0o600,
		"server.log":             0o644, // the official app's, left alone
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	makeLogsPrivate(filepath.Join(dir, "hardllama-app.log"))

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, want 0700", info.Mode().Perm())
	}
	for name, want := range files {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode %v, want %v", name, info.Mode().Perm(), want)
		}
	}
}
