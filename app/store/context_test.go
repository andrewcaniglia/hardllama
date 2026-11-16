//go:build windows || darwin

package store

import (
	"testing"
)

func TestWasTruncated(t *testing.T) {
	prev := &ContextUsage{Model: "m", Prompt: 3000, Reply: 200, Limit: 4096, Messages: 3}
	for _, tt := range []struct {
		name string
		prev *ContextUsage
		u    ContextUsage
		want bool
	}{
		{"fits", prev, ContextUsage{Model: "m", Prompt: 3300, Reply: 200, Limit: 4096, Messages: 5}, false},
		{"sent less as the chat grew", prev, ContextUsage{Model: "m", Prompt: 2500, Reply: 200, Limit: 4096, Messages: 5}, true},
		{"reply ran past the limit", nil, ContextUsage{Model: "m", Prompt: 4000, Reply: 200, Limit: 4096, Messages: 1}, true},
		{"edited to fewer messages", prev, ContextUsage{Model: "m", Prompt: 1000, Reply: 200, Limit: 4096, Messages: 1}, false},
		{"another model counts differently", prev, ContextUsage{Model: "other", Prompt: 2500, Reply: 200, Limit: 4096, Messages: 5}, false},
		{"limit unknown", nil, ContextUsage{Model: "m", Prompt: 9000, Reply: 200, Messages: 1}, false},
	} {
		if got := wasTruncated(tt.prev, tt.u); got != tt.want {
			t.Errorf("%s: got %v", tt.name, got)
		}
	}
}

func TestContextUsageSealedAndDeletedWithChat(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetChat(fullChat("c1")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	u := ContextUsage{Model: "m", Prompt: 1234, Reply: 56, Limit: 4096, Messages: 2}
	if err := s.RecordContextUsage("c1", u); err != nil {
		t.Fatal(err)
	}
	got, err := s.ContextUsage("c1")
	if err != nil || got == nil || *got != u {
		t.Fatalf("ContextUsage = %+v, %v", got, err)
	}

	s.LockVault()
	if _, err := s.ContextUsage("c1"); err == nil {
		t.Fatal("read usage while locked")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertAllSealed(t, s.DBPath)

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()
	if err := reopened.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := reopened.DeleteChat("c1"); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := reopened.db.conn.QueryRow(`SELECT COUNT(*) FROM chat_context`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("usage outlived its chat")
	}
}

func TestPrivateChatContextUsageStaysInMemory(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	chat := fullChat("p1")
	chat.Private = true
	if err := s.SetChat(chat); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordContextUsage("p1", ContextUsage{Model: "m", Prompt: 10}); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.db.conn.QueryRow(`SELECT COUNT(*) FROM chat_context`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("private chat's usage was saved")
	}
	if got, _ := s.ContextUsage("p1"); got == nil || got.Prompt != 10 {
		t.Fatalf("usage = %+v", got)
	}
	s.LockVault()
	if s.private.contextUsage("p1") != nil {
		t.Fatal("private usage kept after locking")
	}
}
