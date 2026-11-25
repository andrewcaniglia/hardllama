//go:build windows || darwin

package ui

import (
	"bytes"
	"encoding/json"

	"net/http"
	"net/http/httptest"

	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/store"
)

func TestChatEventFromApiChatResponseUsesThinkingEvent(t *testing.T) {
	start := time.Now()

	event := chatEventFromApiChatResponse(api.ChatResponse{
		Message: api.Message{
			Thinking: "Let me think",
		},
	}, &start, nil)

	if event.EventName != "thinking" {
		t.Fatalf("event.EventName = %q, want %q", event.EventName, "thinking")
	}

	if event.Thinking == nil || *event.Thinking != "Let me think" {
		t.Fatalf("event.Thinking = %v, want %q", event.Thinking, "Let me think")
	}
}

func TestChatStreamEmitsLoadingKeepaliveDuringIdlePhases(t *testing.T) {
	oldInterval := chatKeepAliveInterval
	chatKeepAliveInterval = 10 * time.Millisecond
	defer func() {
		chatKeepAliveInterval = oldInterval
	}()

	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			time.Sleep(35 * time.Millisecond)
			json.NewEncoder(w).Encode(api.ChatResponse{Done: true, Message: api.Message{Content: "hello"}})
		} else {
			w.Write([]byte("{}"))
		}
	}))
	defer engine.Close()
	t.Setenv("OLLAMA_HOST", engine.URL)

	testStore := &store.Store{
		DBPath: filepath.Join(t.TempDir(), "db.sqlite"),
	}
	defer testStore.Close()

	server := &Server{Store: testStore}

	req := httptest.NewRequest("POST", "/api/v1/chat/chat-1", bytes.NewBufferString(`{"model":"test-model","prompt":"hello"}`))
	req.SetPathValue("id", "chat-1")
	rr := httptest.NewRecorder()

	if err := server.chat(rr, req); err != nil {
		t.Fatalf("chat() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 streamed events, got %d: %q", len(lines), rr.Body.String())
	}

	var eventNames []string
	for _, line := range lines {
		var event struct {
			EventName string `json:"eventName"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("failed to parse streamed event %q: %v", line, err)
		}
		eventNames = append(eventNames, event.EventName)
	}

	loadingIndex := -1
	chatIndex := -1
	doneIndex := -1
	for i, eventName := range eventNames {
		switch eventName {
		case "loading":
			if loadingIndex == -1 {
				loadingIndex = i
			}
		case "chat":
			if chatIndex == -1 {
				chatIndex = i
			}
		case "done":
			if doneIndex == -1 {
				doneIndex = i
			}
		}
	}

	if loadingIndex == -1 {
		t.Fatalf("expected a loading keepalive event, got %v", eventNames)
	}
	if chatIndex == -1 {
		t.Fatalf("expected a chat event, got %v", eventNames)
	}
	if doneIndex == -1 {
		t.Fatalf("expected a done event, got %v", eventNames)
	}
	if eventNames[len(eventNames)-1] != "done" {
		t.Fatalf("expected done to be the final event, got %v", eventNames)
	}
	if !(loadingIndex < chatIndex && chatIndex < doneIndex) {
		t.Fatalf("expected loading before chat before done, got %v", eventNames)
	}
}

func TestChatPreservesPartialReplyOnEarlyEOF(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			json.NewEncoder(w).Encode(api.ChatResponse{Message: api.Message{Role: "assistant", Content: "partial reply"}})
			return
		}
		w.Write([]byte("{}"))
	}))
	defer engine.Close()
	t.Setenv("OLLAMA_HOST", engine.URL)
	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}}
	defer s.Store.Close()
	req := httptest.NewRequest("POST", "/api/v1/chat/interrupted", strings.NewReader(`{"model":"test","prompt":"hello"}`))
	req.SetPathValue("id", "interrupted")
	rec := httptest.NewRecorder()
	if err := s.chat(rec, req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"eventName":"error"`) || strings.Contains(rec.Body.String(), `"eventName":"done"`) {
		t.Fatal("missing interruption error", rec.Body.String())
	}
	chat, err := s.Store.Chat("interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 2 || chat.Messages[1].Content != "partial reply" {
		t.Fatal("partial reply was lost")
	}
}
