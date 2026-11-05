//go:build windows || darwin

package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ollama/ollama/app/types/not"
)

const secret = "sentinel-5f1c-do-not-persist"

// assertNotOnDisk scans every file beside the database, including the WAL.
func assertNotOnDisk(t *testing.T, s *Store) {
	t.Helper()

	err := filepath.WalkDir(filepath.Dir(s.DBPath), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("found secret in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func privateTestStore(t *testing.T) *Store {
	t.Helper()

	s := &Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}
	t.Cleanup(func() { s.Close() })

	// Open the database so that scanning it is meaningful.
	if _, err := s.Chats(); err != nil {
		t.Fatal(err)
	}
	return s
}

func newPrivateChat(t *testing.T, s *Store, id string) {
	t.Helper()

	chat := NewChat(id)
	chat.Private = true
	chat.Messages = append(chat.Messages, NewMessage("user", secret, nil))
	if err := s.SetChat(*chat); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateChatNeverReachesDisk(t *testing.T) {
	s := privateTestStore(t)
	newPrivateChat(t, s, "private")

	if err := s.AppendMessage("private", NewMessage("assistant", "", nil)); err != nil {
		t.Fatal(err)
	}
	reply := NewMessage("assistant", secret, nil)
	reply.Thinking = secret
	if err := s.UpdateLastMessage("private", reply); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChatBrowserState("private", []byte(`"`+secret+`"`)); err != nil {
		t.Fatal(err)
	}

	chat, err := s.Chat("private")
	if err != nil {
		t.Fatal(err)
	}
	if !chat.Private || len(chat.Messages) != 2 || chat.Messages[1].Content != secret {
		t.Fatalf("unexpected chat: %+v", chat)
	}

	chats, err := s.Chats()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || !chats[0].Private {
		t.Fatalf("expected private chat in list, got %+v", chats)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertNotOnDisk(t, s)
}

func TestPrivateChatGoneAfterRestart(t *testing.T) {
	s := privateTestStore(t)
	newPrivateChat(t, s, "private")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()
	if _, err := reopened.Chat("private"); !errors.Is(err, not.Found) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestPrivateChatWritesAfterDeleteAreDropped(t *testing.T) {
	s := privateTestStore(t)
	newPrivateChat(t, s, "private")

	// The handler holds its own copy of the chat while a response streams.
	inFlight, err := s.Chat("private")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChat("private"); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendMessage("private", NewMessage("assistant", secret, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateLastMessage("private", NewMessage("assistant", secret, nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(*inFlight); err != nil {
		t.Fatal(err)
	}

	// A stale copy that lost its private flag must not persist either.
	inFlight.Private = false
	if err := s.SetChat(*inFlight); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Chat("private"); !errors.Is(err, not.Found) {
		t.Fatalf("expected not found, got %v", err)
	}
	chats, err := s.Chats()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 0 {
		t.Fatalf("expected no chats, got %+v", chats)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertNotOnDisk(t, s)
}

func TestPrivateChatIsolatedFromCallerMutation(t *testing.T) {
	s := privateTestStore(t)
	newPrivateChat(t, s, "private")

	chat, err := s.Chat("private")
	if err != nil {
		t.Fatal(err)
	}
	chat.Messages[0].Content = "changed"

	stored, err := s.Chat("private")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Messages[0].Content != secret {
		t.Fatalf("store was mutated: %q", stored.Messages[0].Content)
	}
}

func TestDeletedChatIsScrubbedFromDatabase(t *testing.T) {
	s := privateTestStore(t)

	// Keep a second chat so that deleted pages are reused rather than dropped.
	keep := NewChat("keep")
	keep.Messages = append(keep.Messages, NewMessage("user", "keep", nil))
	if err := s.SetChat(*keep); err != nil {
		t.Fatal(err)
	}

	chat := NewChat("deleted")
	chat.Messages = append(chat.Messages, NewMessage("user", secret, nil))
	if err := s.SetChat(*chat); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChat("deleted"); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	assertNotOnDisk(t, s)
}
