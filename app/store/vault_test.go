//go:build windows || darwin

package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	vaultSecret   = "sentinel-c41b-vault-plaintext"
	vaultPassword = "correct horse battery"
)

func assertVaultSecretNotOnDisk(t *testing.T, dbPath string) {
	t.Helper()

	files, err := filepath.Glob(dbPath + "*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no database files found")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(vaultSecret)) {
			t.Errorf("found plaintext in %s", file)
		}
	}
}

// fullChat fills every sealed column with the secret.
func fullChat(id string) Chat {
	result := json.RawMessage(`"` + vaultSecret + `"`)
	chat := NewChat(id)
	chat.Title = vaultSecret
	chat.BrowserState = json.RawMessage(`"` + vaultSecret + `"`)

	msg := NewMessage("user", vaultSecret, &MessageOptions{
		Thinking:    vaultSecret,
		Attachments: []File{{Filename: vaultSecret + ".txt", Data: []byte(vaultSecret)}},
	})
	msg.ToolResult = &result
	msg.ToolCalls = []ToolCall{{
		Type:     "function",
		Function: ToolFunction{Name: "tool", Arguments: vaultSecret, Result: &result},
	}}
	chat.Messages = append(chat.Messages, msg)
	return *chat
}

func assertFullChat(t *testing.T, chat *Chat) {
	t.Helper()

	if chat.Title != vaultSecret || !strings.Contains(string(chat.BrowserState), vaultSecret) {
		t.Fatalf("chat metadata not restored: %+v", chat)
	}
	if len(chat.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(chat.Messages))
	}
	msg := chat.Messages[0]
	if msg.Content != vaultSecret || msg.Thinking != vaultSecret {
		t.Fatalf("message not restored: %+v", msg)
	}
	if msg.ToolResult == nil || !strings.Contains(string(*msg.ToolResult), vaultSecret) {
		t.Fatalf("tool result not restored: %+v", msg)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != vaultSecret+".txt" || string(msg.Attachments[0].Data) != vaultSecret {
		t.Fatalf("attachment not restored: %+v", msg.Attachments)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Arguments != vaultSecret {
		t.Fatalf("tool call not restored: %+v", msg.ToolCalls)
	}
	result, err := json.Marshal(msg.ToolCalls[0].Function.Result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result), vaultSecret) {
		t.Fatalf("tool call result not restored: %s", result)
	}
}

func TestVaultEncryptsExistingChats(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetChat(fullChat("existing")); err != nil {
		t.Fatal(err)
	}

	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	assertVaultSecretNotOnDisk(t, s.DBPath)

	chat, err := s.Chat("existing")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, chat)
}

func TestVaultEncryptsNewWrites(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}

	if err := s.SetChat(fullChat("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessage("new", NewMessage("assistant", vaultSecret, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateLastMessage("new", NewMessage("assistant", vaultSecret+" updated", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChatBrowserState("new", json.RawMessage(`"`+vaultSecret+`"`)); err != nil {
		t.Fatal(err)
	}
	assertVaultSecretNotOnDisk(t, s.DBPath)

	chats, err := s.Chats()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].Title != vaultSecret || chats[0].Messages[0].Content != vaultSecret {
		t.Fatalf("sidebar data not restored: %+v", chats)
	}

	chat, err := s.Chat("new")
	if err != nil {
		t.Fatal(err)
	}
	if got := chat.Messages[1].Content; got != vaultSecret+" updated" {
		t.Fatalf("unexpected last message: %q", got)
	}
}

func TestVaultLockedAfterRestart(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetChat(fullChat("existing")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()

	enabled, locked, err := reopened.VaultStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || !locked {
		t.Fatalf("expected locked vault, got enabled=%v locked=%v", enabled, locked)
	}

	if err := reopened.UnlockVault("wrong password"); !errors.Is(err, ErrVaultPassword) {
		t.Fatalf("expected password error, got %v", err)
	}
	if _, locked, _ := reopened.VaultStatus(); !locked {
		t.Fatal("wrong password unlocked the vault")
	}

	if err := reopened.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	chat, err := reopened.Chat("existing")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, chat)
}

// Reads that fail while locked must not look like a missing chat, because the
// chat handler responds to that by replacing it.
func TestVaultLockedRefusesAccess(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetChat(fullChat("existing")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	s.LockVault()

	chat := NewChat("existing")
	chat.Messages = append(chat.Messages, NewMessage("user", vaultSecret+" late", nil))
	calls := map[string]func() error{
		"Chats":      func() error { _, err := s.Chats(); return err },
		"Chat":       func() error { _, err := s.Chat("existing"); return err },
		"SetChat":    func() error { return s.SetChat(*chat) },
		"DeleteChat": func() error { return s.DeleteChat("existing") },
		"Append":     func() error { return s.AppendMessage("existing", NewMessage("user", vaultSecret, nil)) },
		"Update":     func() error { return s.UpdateLastMessage("existing", NewMessage("user", vaultSecret, nil)) },
		"Browser":    func() error { return s.UpdateChatBrowserState("existing", json.RawMessage(`"x"`)) },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrVaultLocked) {
			t.Errorf("%s: expected locked error, got %v", name, err)
		}
	}
	assertVaultSecretNotOnDisk(t, s.DBPath)

	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Chat("existing")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, restored)
}

// The database layer must refuse too, for writes racing with a lock.
func TestVaultSealFailsClosed(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	s.LockVault()

	if err := s.db.saveChat(fullChat("raced")); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("expected locked error, got %v", err)
	}
	empty := NewChat("empty")
	if err := s.db.saveChat(*empty); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("expected locked error for empty chat, got %v", err)
	}
	assertVaultSecretNotOnDisk(t, s.DBPath)
}

func TestVaultLockClearsPrivateChats(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	newPrivateChat(t, s, "private")

	s.LockVault()
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if chat, ok := s.private.get("private"); !ok || chat != nil {
		t.Fatalf("expected private chat to be erased, got %+v", chat)
	}
}

func TestVaultRejectsTampering(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(fullChat("existing")); err != nil {
		t.Fatal(err)
	}

	// A sealed title moved into another column must not decrypt.
	if _, err := s.db.conn.Exec(`UPDATE messages SET content = (SELECT title FROM chats)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chat("existing"); !errors.Is(err, ErrVaultDecryptFail) {
		t.Fatalf("expected decrypt failure, got %v", err)
	}
}

func TestVaultSetupRules(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault("short"); !errors.Is(err, ErrVaultWeakPass) {
		t.Fatalf("expected weak password error, got %v", err)
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.SetupVault(vaultPassword); !errors.Is(err, ErrVaultEnabled) {
		t.Fatalf("expected enabled error, got %v", err)
	}
}

func TestVaultIdleTimerLocks(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}

	s.vaultTimerMu.Lock()
	s.vaultTimer.Reset(time.Millisecond)
	s.vaultTimerMu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, locked, _ := s.VaultStatus(); locked {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("vault did not lock when idle")
}

func TestVaultWithoutSetupStaysPlaintext(t *testing.T) {
	s := privateTestStore(t)
	chat := fullChat("existing")
	chat.Title = "ollama-vault-1:not sealed"
	if err := s.SetChat(chat); err != nil {
		t.Fatal(err)
	}

	got, err := s.Chat("existing")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != chat.Title {
		t.Fatalf("unexpected title: %q", got.Title)
	}
}

func TestVaultConcurrentWritesDuringSetup(t *testing.T) {
	s := privateTestStore(t)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 10 {
				_ = s.SetChat(fullChat(fmt.Sprintf("chat-%d-%d", i, j)))
			}
		}()
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertVaultSecretNotOnDisk(t, s.DBPath)
}

// Databases from before secure_delete still hold deleted text in free pages.
func TestVaultSetupRemovesPreviouslyDeletedText(t *testing.T) {
	s := privateTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite3", s.DBPath+"?_secure_delete=off")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO chats (id, title) VALUES ('old', ?)`, vaultSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM chats WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(vaultSecret)) {
		t.Fatal("test setup expected deleted text to remain in the file")
	}

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()
	if err := reopened.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	assertVaultSecretNotOnDisk(t, s.DBPath)
}
