//go:build windows || darwin

package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ollama/ollama/app/share"
)

// Sharing has to keep working while the sharing computer is locked, but
// changing who it shares with must not.
func TestShareDevices(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}

	if enabled, err := s.ShareEnabled(); err != nil || enabled {
		t.Fatalf("sharing on by default: %v %v", enabled, err)
	}
	if err := s.SetShareEnabled(true); err != nil {
		t.Fatal(err)
	}

	s.LockVault()
	if enabled, err := s.ShareEnabled(); err != nil || !enabled {
		t.Fatalf("sharing setting unreadable while locked: %v %v", enabled, err)
	}
	if err := s.SetShareEnabled(false); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("sharing turned off while locked: %v", err)
	}

	// A pairing that finishes after locking is still saved.
	device := share.Device{ID: "one", Name: "Mac", TokenHash: share.HashToken("secret"), CreatedAt: time.Now()}
	if err := s.AddShareDevice(device); err != nil {
		t.Fatal(err)
	}
	devices, err := s.ShareDevices()
	if err != nil || len(devices) != 1 || devices[0].ID != "one" || string(devices[0].TokenHash) != string(device.TokenHash) {
		t.Fatalf("devices unreadable while locked: %+v %v", devices, err)
	}
	if err := s.RemoveShareDevice("one"); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("device removed while locked: %v", err)
	}

	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveShareDevice("one"); err != nil {
		t.Fatal(err)
	}
	if devices, _ := s.ShareDevices(); len(devices) != 0 {
		t.Fatalf("device not removed: %+v", devices)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertValuesNotOnDisk(t, s.DBPath, "secret")
}

func TestDataFolderIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "HardLlama")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Store{DBPath: filepath.Join(dir, "db.sqlite")}
	defer s.Close()
	if _, err := s.ID(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("data folder mode %v, want 0700", info.Mode().Perm())
	}
}
