//go:build windows || darwin

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ollama/ollama/app/store"
)

// leaveTestStore makes an unlocked store with the given lock delay, and
// records the delay leftApp waits instead of actually waiting.
func leaveTestStore(t *testing.T, delay time.Duration) (s *store.Store, waited *time.Duration) {
	t.Helper()
	s = &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}
	if err := s.SetupVault("correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLockDelay(delay); err != nil {
		t.Fatal(err)
	}
	previousStore, previousAfter := appStore, afterFunc
	appStore = s
	waited = new(time.Duration)
	afterFunc = func(d time.Duration, f func()) *time.Timer {
		*waited = d
		return time.AfterFunc(10*time.Millisecond, f)
	}
	t.Cleanup(func() {
		returnedToApp()
		appStore, afterFunc = previousStore, previousAfter
		s.Close()
	})
	return s, waited
}

func locked(t *testing.T, s *store.Store) bool {
	t.Helper()
	_, locked, err := s.VaultStatus()
	if err != nil {
		t.Fatal(err)
	}
	return locked
}

func TestLeavingLocksAfterTheChosenDelay(t *testing.T) {
	s, waited := leaveTestStore(t, 5*time.Minute)
	leftApp()
	if *waited != 5*time.Minute {
		t.Fatalf("waited %v, want 5m", *waited)
	}
	if locked(t, s) {
		t.Fatal("locked before the delay")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !locked(t, s) {
		if time.Now().After(deadline) {
			t.Fatal("didn't lock after leaving")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLeavingLocksInstantly(t *testing.T) {
	s, waited := leaveTestStore(t, 0)
	leftApp()
	if !locked(t, s) {
		t.Fatal("didn't lock as soon as the user left")
	}
	if *waited != 0 {
		t.Fatalf("set a timer for %v", *waited)
	}
}

func TestComingBackKeepsChatsUnlocked(t *testing.T) {
	s, _ := leaveTestStore(t, 5*time.Second)
	leftApp()
	returnedToApp()
	time.Sleep(50 * time.Millisecond)
	if locked(t, s) {
		t.Fatal("locked even though the user came back in time")
	}
}
