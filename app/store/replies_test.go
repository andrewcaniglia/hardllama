//go:build windows || darwin

package store

import (
	"bytes"
	"testing"
)

func replyChat(t *testing.T, s *Store, withReply bool) {
	t.Helper()
	chat := NewChat("c1")
	chat.Messages = append(chat.Messages, NewMessage("user", "question", nil))
	if withReply {
		chat.Messages = append(chat.Messages, NewMessage("assistant", "start", nil))
	}
	if err := s.SetChat(*chat); err != nil {
		t.Fatal(err)
	}
}

func TestReplyWrittenWhileLockedIsSealed(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	replyChat(t, s, true)
	s.LockVault()

	for _, piece := range []string{" sensitive", " words"} {
		if err := s.ContinueReply("c1", "m", piece, ""); err != nil {
			t.Fatal(err)
		}
	}
	if !s.ReplyFinishing("c1") {
		t.Fatal("not finishing")
	}
	s.FinishReply("c1")
	for _, sealed := range s.pending.replies["c1"].pieces {
		if bytes.Contains(sealed, []byte("sensitive")) {
			t.Fatal("reply is held in plain text while locked")
		}
	}

	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Chat("c1")
	if err != nil {
		t.Fatal(err)
	}
	if got := chat.Messages[1].Content; got != "start sensitive words" {
		t.Fatalf("reply = %q", got)
	}
	if len(s.pending.replies) != 0 {
		t.Fatal("finished reply kept after unlocking")
	}
}

func TestReplyLockedTwice(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	replyChat(t, s, true)

	s.LockVault()
	s.ContinueReply("c1", "m", " one", "")
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	// Unlocked, the next piece is added right away.
	s.ContinueReply("c1", "m", " two", "")
	s.LockVault()
	s.ContinueReply("c1", "m", " three", "")
	s.FinishReply("c1")
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	chat, _ := s.Chat("c1")
	if got := chat.Messages[1].Content; got != "start one two three" {
		t.Fatalf("reply = %q", got)
	}
}

func TestReplyDroppedIfChatChanged(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	replyChat(t, s, true)
	s.LockVault()
	s.ContinueReply("c1", "m", " stray", "")
	s.FinishReply("c1")
	// As if the chat had gained messages since the reply was cut off.
	s.pending.replies["c1"].messages = 5

	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	chat, _ := s.Chat("c1")
	if got := chat.Messages[1].Content; got != "start" {
		t.Fatalf("added to the wrong message: %q", got)
	}
	if len(s.pending.replies) != 0 {
		t.Fatal("kept a reply that can't be added")
	}
}

func TestReplyForADeletedChat(t *testing.T) {
	s := privateTestStore(t)
	defer s.Close()
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	replyChat(t, s, true)
	if err := s.DeleteChat("c1"); err != nil {
		t.Fatal(err)
	}
	s.LockVault()
	s.ContinueReply("c1", "m", " stray", "")
	s.FinishReply("c1")
	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chat("c1"); err == nil {
		t.Fatal("deleted chat came back")
	}
}
