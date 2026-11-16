//go:build windows || darwin

package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

const draftText = "half-written prompt about something sensitive"

func lockedWithDraft(t *testing.T) *Store {
	t.Helper()
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	s.LockVault()
	// The window saves the draft after chats have locked.
	if err := s.SaveDraft("chat-1", draftText); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDraftSurvivesLock(t *testing.T) {
	s := lockedWithDraft(t)
	defer s.Close()

	if _, err := s.TakeDraft("chat-1"); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("read a draft while locked: %v", err)
	}
	for _, sealed := range s.drafts.saved {
		if bytes.Contains(sealed, []byte("sensitive")) {
			t.Fatal("draft is kept in plaintext while locked")
		}
	}

	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakeDraft("chat-1")
	if err != nil || got != draftText {
		t.Fatalf("TakeDraft = %q, %v", got, err)
	}
	// It's handed back once.
	if got, err := s.TakeDraft("chat-1"); err != nil || got != "" {
		t.Fatalf("second TakeDraft = %q, %v", got, err)
	}
	if got, err := s.TakeDraft("chat-2"); err != nil || got != "" {
		t.Fatalf("other chat's draft = %q, %v", got, err)
	}
}

func TestDraftSurvivesLockingTwice(t *testing.T) {
	s := lockedWithDraft(t)
	defer s.Close()

	// Unlocking keeps the key, so a draft saved before an earlier unlock
	// can still be read.
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	s.LockVault()
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if got, err := s.TakeDraft("chat-1"); err != nil || got != draftText {
		t.Fatalf("TakeDraft = %q, %v", got, err)
	}
}

func TestDraftSurvivesPasswordChange(t *testing.T) {
	s := lockedWithDraft(t)
	defer s.Close()
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeVaultPassword(vaultPassword, newVaultPassword); err != nil {
		t.Fatal(err)
	}
	if got, err := s.TakeDraft("chat-1"); err != nil || got != draftText {
		t.Fatalf("TakeDraft = %q, %v", got, err)
	}
}

func TestDraftOnlyOpensForItsChat(t *testing.T) {
	s := lockedWithDraft(t)
	defer s.Close()
	s.drafts.saved["chat-2"] = s.drafts.saved["chat-1"]
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakeDraft("chat-2"); !errors.Is(err, ErrVaultDecryptFail) {
		t.Fatalf("moved draft opened: %v", err)
	}
}

func TestDraftKeyIsSealed(t *testing.T) {
	s := lockedWithDraft(t)
	defer s.Close()
	if !bytes.HasPrefix(s.drafts.sealedKey, sealedPrefix) {
		t.Fatal("draft key isn't sealed with the vault key")
	}
	if _, err := s.drafts.privateKey(nil); err != nil {
		t.Fatal(err)
	}
}

func TestDraftLimits(t *testing.T) {
	s := lockedWithDraft(t)
	defer s.Close()

	if err := s.SaveDraft("big", strings.Repeat("x", maxDraftSize+1)); !errors.Is(err, ErrDraftTooLarge) {
		t.Fatalf("oversized draft: %v", err)
	}
	// An empty draft removes it.
	if err := s.SaveDraft("chat-1", ""); err != nil {
		t.Fatal(err)
	}
	if len(s.drafts.saved) != 0 {
		t.Fatal("empty draft didn't remove the saved one")
	}
}

func TestDraftNeedsUnlockSinceStart(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	reopened := &Store{DBPath: s.DBPath}
	s.Close()
	defer reopened.Close()
	if err := reopened.SaveDraft("chat-1", draftText); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("saved a draft with no key: %v", err)
	}
}

func TestTouchVaultDelaysIdleLock(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	s.vaultTimerMu.Lock()
	timer := s.vaultTimer
	s.vaultTimerMu.Unlock()
	// Reset reports whether the timer was still pending, so stopping it
	// first shows whether TouchVault restarts it.
	timer.Stop()
	s.TouchVault()
	if !timer.Stop() {
		t.Fatal("TouchVault didn't restart the idle timer")
	}
}

func TestLockDelay(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if d, err := s.LockDelay(); err != nil || d != 5*time.Second {
		t.Fatalf("default = %v, %v", d, err)
	}
	if err := s.SetLockDelay(10 * time.Second); !errors.Is(err, ErrBadLockDelay) {
		t.Fatalf("accepted 10s: %v", err)
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLockDelay(0); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.LockDelay(); d != 0 {
		t.Fatalf("instant = %v", d)
	}
	if err := s.SetLockDelay(time.Minute); err != nil {
		t.Fatal(err)
	}
	s.LockVault()
	// Someone at a locked computer can't keep it unlocked longer next time.
	if err := s.SetLockDelay(15 * time.Second); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("changed while locked: %v", err)
	}
	if d, _ := s.LockDelay(); d != time.Minute {
		t.Fatalf("delay = %v", d)
	}

	// A value that isn't a choice uses the shortest.
	if _, err := s.db.conn.Exec(`UPDATE hardllama_settings SET lock_delay_seconds = 3600`); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.LockDelay(); d != 0 {
		t.Fatalf("bad value gave %v, want instantly", d)
	}
}

func TestLockDelayAddedToOldDatabases(t *testing.T) {
	s := privateTestStore(t)
	if err := s.ensureDB(); err != nil {
		t.Fatal(err)
	}
	// As made by the previous version.
	if _, err := s.db.conn.Exec(`ALTER TABLE hardllama_settings DROP COLUMN lock_delay_seconds`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()
	if d, err := reopened.LockDelay(); err != nil || d != 5*time.Second {
		t.Fatalf("LockDelay = %v, %v", d, err)
	}
}
