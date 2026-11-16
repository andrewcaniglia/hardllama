//go:build windows || darwin

package ui

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/app/ui/responses"
)

const afterLockSecret = "written-after-locking-7c1f"

// replyEngine streams a reply, locking chats after lockAfter pieces, and
// waits for release before sending the rest.
type replyEngine struct {
	pieces    []api.Message
	lockAfter int
	lock      func()
	release   chan struct{}
	finished  chan struct{}
}

func (e *replyEngine) serve(t *testing.T) {
	t.Helper()
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			json.NewEncoder(w).Encode(api.ShowResponse{})
		case "/api/chat":
			enc := json.NewEncoder(w)
			for i, m := range e.pieces {
				if i == e.lockAfter {
					e.lock()
					if e.release != nil {
						<-e.release
					}
				}
				if err := enc.Encode(api.ChatResponse{Message: m}); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
			enc.Encode(api.ChatResponse{Done: true})
			if e.finished != nil {
				close(e.finished)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ollama.Close)
	t.Setenv("OLLAMA_HOST", ollama.URL)
}

func lockedReplyServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}}
	t.Cleanup(func() { s.Store.Close() })
	if err := s.Store.SetupVault(testPassword); err != nil {
		t.Fatal(err)
	}
	return s
}

// send sends a message and returns the new chat's ID and what the window
// was sent.
func send(t *testing.T, s *Server, ctx context.Context, private bool) (string, string) {
	t.Helper()
	body, _ := json.Marshal(responses.ChatRequest{Model: "test", Prompt: "hi", Private: private})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/chat/new", bytes.NewReader(body))
	req.SetPathValue("id", "new")
	rec := httptest.NewRecorder()
	if err := s.chat(rec, req); err != nil {
		t.Fatal(err)
	}
	id := ""
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	for {
		var event responses.ChatEvent
		if err := dec.Decode(&event); err != nil {
			break
		}
		if event.EventName == "chat_created" && event.ChatID != nil {
			id = *event.ChatID
		}
	}
	return id, rec.Body.String()
}

func assertNotOnDisk(t *testing.T, s *store.Store, secret string) {
	t.Helper()
	// Checkpoint so nothing hides in the WAL, then read the files raw.
	raw, err := sql.Open("sqlite3", s.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	raw.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	raw.Close()
	files, _ := filepath.Glob(s.DBPath + "*")
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("%s holds the reply in plain text", filepath.Base(f))
		}
	}
}

func TestReplyFinishesAfterLocking(t *testing.T) {
	s := lockedReplyServer(t)
	e := &replyEngine{
		pieces: []api.Message{
			{Role: "assistant", Content: "first part"},
			{Role: "assistant", Content: " " + afterLockSecret},
			{Role: "assistant", Content: " end"},
		},
		lockAfter: 1,
		lock:      s.Store.LockVault,
	}
	e.serve(t)

	id, window := send(t, s, context.Background(), false)
	if strings.Contains(window, afterLockSecret) {
		t.Fatal("the window was sent the reply after chats locked")
	}
	if !strings.Contains(window, `"eventName":"done"`) {
		t.Fatal("the window wasn't told the reply was done")
	}
	if strings.Contains(window, `"eventName":"error"`) {
		t.Fatalf("reported an error: %s", window)
	}
	assertNotOnDisk(t, s.Store, afterLockSecret)

	if err := s.Store.UnlockVault(testPassword); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Store.Chat(id)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(chat.Messages); n != 2 || chat.Messages[1].Content != "first part "+afterLockSecret+" end" {
		t.Fatalf("reply after unlocking: %+v", chat.Messages)
	}
	if s.Store.ReplyFinishing(id) {
		t.Fatal("still finishing")
	}
}

func TestReplyStartingAfterLockIsAdded(t *testing.T) {
	s := lockedReplyServer(t)
	e := &replyEngine{
		pieces: []api.Message{
			{Role: "assistant", Thinking: "hmm"},
			{Role: "assistant", Content: afterLockSecret},
		},
		lockAfter: 0,
		lock:      s.Store.LockVault,
	}
	e.serve(t)

	id, _ := send(t, s, context.Background(), false)
	if err := s.Store.UnlockVault(testPassword); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Store.Chat(id)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(chat.Messages); n != 2 || chat.Messages[1].Role != "assistant" ||
		chat.Messages[1].Content != afterLockSecret || chat.Messages[1].Thinking != "hmm" || chat.Messages[1].Model != "test" {
		t.Fatalf("reply after unlocking: %+v", chat.Messages)
	}
}

func TestReplyKeepsGoingWhenTheWindowLeaves(t *testing.T) {
	s := lockedReplyServer(t)
	ctx, leave := context.WithCancel(context.Background())
	e := &replyEngine{
		pieces: []api.Message{
			{Role: "assistant", Content: "first part"},
			{Role: "assistant", Content: " " + afterLockSecret},
		},
		lockAfter: 1,
		release:   make(chan struct{}),
		finished:  make(chan struct{}),
	}
	e.lock = func() {
		s.Store.LockVault()
		// The window shows the lock screen and stops listening.
		go func() {
			time.Sleep(20 * time.Millisecond)
			leave()
			time.Sleep(20 * time.Millisecond)
			close(e.release)
		}()
	}
	e.serve(t)

	id, _ := send(t, s, ctx, false)
	<-e.finished

	// Unlocking while the reply is still being written adds the rest as it
	// arrives.
	if err := s.Store.UnlockVault(testPassword); err != nil {
		t.Fatal(err)
	}
	chat, _ := s.Store.Chat(id)
	if got := chat.Messages[len(chat.Messages)-1].Content; got != "first part "+afterLockSecret {
		t.Fatalf("reply = %q", got)
	}
}

func TestLeavingStillStopsAReplyWhileUnlocked(t *testing.T) {
	s := lockedReplyServer(t)
	ctx, leave := context.WithCancel(context.Background())
	e := &replyEngine{
		pieces: []api.Message{
			{Role: "assistant", Content: "first part"},
			{Role: "assistant", Content: " " + afterLockSecret},
		},
		lockAfter: 1,
		release:   make(chan struct{}),
	}
	e.lock = func() {
		// Instead of locking, the user stops the reply.
		leave()
		time.Sleep(50 * time.Millisecond)
		close(e.release)
	}
	e.serve(t)

	id, _ := send(t, s, ctx, false)
	chat, _ := s.Store.Chat(id)
	if got := chat.Messages[len(chat.Messages)-1].Content; strings.Contains(got, afterLockSecret) {
		t.Fatalf("stopping didn't stop the reply: %q", got)
	}
}

func TestReplyStopsAtAToolWhileLocked(t *testing.T) {
	s := lockedReplyServer(t)
	e := &replyEngine{
		pieces: []api.Message{
			{Role: "assistant", Content: "first part"},
			{Role: "assistant", Content: " more"},
			{Role: "assistant", ToolCalls: []api.ToolCall{{Function: api.ToolCallFunction{Name: "web_search"}}}},
			{Role: "assistant", Content: " " + afterLockSecret},
		},
		lockAfter: 1,
		lock:      s.Store.LockVault,
	}
	e.serve(t)

	id, _ := send(t, s, context.Background(), false)
	if err := s.Store.UnlockVault(testPassword); err != nil {
		t.Fatal(err)
	}
	chat, _ := s.Store.Chat(id)
	if got := chat.Messages[len(chat.Messages)-1].Content; got != "first part more" {
		t.Fatalf("reply = %q", got)
	}
}

func TestPrivateReplyStopsOnLock(t *testing.T) {
	s := lockedReplyServer(t)
	e := &replyEngine{
		pieces: []api.Message{
			{Role: "assistant", Content: "first part"},
			{Role: "assistant", Content: " " + afterLockSecret},
		},
		lockAfter: 1,
		lock:      s.Store.LockVault,
	}
	e.serve(t)

	_, window := send(t, s, context.Background(), true)
	if !strings.Contains(window, `"code":"vault_locked"`) {
		t.Fatalf("private reply didn't stop: %s", window)
	}
	if strings.Contains(window, afterLockSecret) {
		t.Fatal("the window was sent a private reply after chats locked")
	}
}
