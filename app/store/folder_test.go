//go:build windows || darwin

package store

import (
	"path/filepath"
	"testing"
)

func TestEncryptedFolderSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.sqlite")
	s := &Store{DBPath: path}
	const password = "folder test password"
	const snapshot = `{"name":"secret-folder","passages":[{"text":"hidden history"}]}`
	if err := s.SetupVault(password); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatWithFolder(*NewChat("saved"), snapshot); err != nil {
		t.Fatal(err)
	}
	private := NewChat("temporary")
	private.Private = true
	if err := s.SetChatWithFolder(*private, snapshot); err != nil {
		t.Fatal(err)
	}
	s.LockVault()
	if _, err := s.FolderSnapshot("saved"); err != ErrVaultLocked {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertValuesNotOnDisk(t, path, "hidden history", "secret-folder")
	reopened := &Store{DBPath: path}
	defer reopened.Close()
	if err := reopened.UnlockVault(password); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"saved": snapshot, "temporary": ""} {
		got, err := reopened.FolderSnapshot(id)
		if err != nil || got != want {
			t.Fatalf("restore %s: %q, %v", id, got, err)
		}
	}
	if err := reopened.SetFolderSnapshot("saved", ""); err != nil {
		t.Fatal(err)
	}
	if got, err := reopened.FolderSnapshot("saved"); err != nil || got != "" {
		t.Fatal("folder removal failed", err)
	}
}
