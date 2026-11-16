//go:build windows || darwin

package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const newVaultPassword = "a different long password"

func TestVaultChangePassword(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetChat(fullChat("content")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(detailChat("details")); err != nil {
		t.Fatal(err)
	}
	setDetailSettings(t, s)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeVaultPassword(vaultPassword, newVaultPassword); err != nil {
		t.Fatal(err)
	}

	// Still unlocked, and everything still reads.
	chat, err := s.Chat("content")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, chat)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertAllSealed(t, s.DBPath)
	assertVaultSecretNotOnDisk(t, s.DBPath)
	assertValuesNotOnDisk(t, s.DBPath, vaultDetailSecret, vaultDetailDate)

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()
	if err := reopened.UnlockVault(vaultPassword); !errors.Is(err, ErrVaultPassword) {
		t.Fatalf("old password: expected password error, got %v", err)
	}
	if err := reopened.UnlockVault(newVaultPassword); err != nil {
		t.Fatal(err)
	}
	chat, err = reopened.Chat("content")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, chat)
	details, err := reopened.Chat("details")
	if err != nil {
		t.Fatal(err)
	}
	assertDetailChat(t, details)
	assertDetailSettings(t, reopened)
}

// Someone with an old copy of the database and the old password must not be
// able to decrypt anything after the password changes.
func TestVaultChangePasswordReplacesKey(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(fullChat("content")); err != nil {
		t.Fatal(err)
	}
	old, err := s.db.unwrapVaultKey(vaultPassword)
	if err != nil {
		t.Fatal(err)
	}
	var oldWrapped []byte
	if err := s.db.conn.QueryRow(`SELECT wrapped_key FROM vault`).Scan(&oldWrapped); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeVaultPassword(vaultPassword, newVaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(fullChat("after")); err != nil {
		t.Fatal(err)
	}

	for _, col := range sealedColumns {
		rows, err := s.db.conn.Query(fmt.Sprintf(`SELECT CAST(%s AS BLOB) FROM %s WHERE %[1]s IS NOT NULL AND length(%[1]s) > 0`, col.column, col.table))
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value []byte
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			if _, err := openValue(old, col.column, value); err == nil {
				t.Errorf("%s.%s still decrypts with the old key", col.table, col.column)
			}
		}
		rows.Close()
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(s.DBPath + "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, oldWrapped) {
			t.Errorf("old wrapped key still in %s", file)
		}
	}
}

func TestVaultChangePasswordRules(t *testing.T) {
	s := privateTestStore(t)
	if err := s.ChangeVaultPassword(vaultPassword, newVaultPassword); !errors.Is(err, ErrVaultNotEnabled) {
		t.Fatalf("without a vault: expected not enabled error, got %v", err)
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeVaultPassword("wrong password!", newVaultPassword); !errors.Is(err, ErrVaultPassword) {
		t.Fatalf("wrong password: expected password error, got %v", err)
	}
	if err := s.ChangeVaultPassword(vaultPassword, "11-char-pwd"); !errors.Is(err, ErrVaultWeakPass) {
		t.Fatalf("weak password: expected weak password error, got %v", err)
	}

	// Neither changed anything.
	s.LockVault()
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
}

// If any value can't be re-encrypted, the old password and key must keep
// working.
func TestVaultChangePasswordFailureChangesNothing(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(fullChat("good")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(fullChat("bad")); err != nil {
		t.Fatal(err)
	}
	// A sealed title moved into another column doesn't decrypt.
	if _, err := s.db.conn.Exec(`UPDATE messages SET content = (SELECT title FROM chats WHERE id = 'bad') WHERE chat_id = 'bad'`); err != nil {
		t.Fatal(err)
	}

	if err := s.ChangeVaultPassword(vaultPassword, newVaultPassword); !errors.Is(err, ErrVaultDecryptFail) {
		t.Fatalf("expected decrypt failure, got %v", err)
	}
	chat, err := s.Chat("good")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, chat)

	s.LockVault()
	if err := s.UnlockVault(newVaultPassword); !errors.Is(err, ErrVaultPassword) {
		t.Fatalf("new password worked after a failed change: %v", err)
	}
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if chat, err = s.Chat("good"); err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, chat)
}

// Writes during a change must all end up readable with the new key.
func TestVaultConcurrentWritesDuringChange(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}

	// Writers run from before the change starts until after it finishes.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var saved []string
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("chat-%d-%d", i, j)
				if s.SetChat(fullChat(id)) == nil {
					mu.Lock()
					saved = append(saved, id)
					mu.Unlock()
				}
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	if err := s.ChangeVaultPassword(vaultPassword, newVaultPassword); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	s.LockVault()
	if err := s.UnlockVault(newVaultPassword); err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 {
		t.Fatal("no writes succeeded")
	}
	for _, id := range saved {
		chat, err := s.Chat(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		assertFullChat(t, chat)
	}
}
