//go:build windows || darwin

package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/app/ui/responses"
)

func TestCustomInstructionsSentAsSystemPrompt(t *testing.T) {
	var mu sync.Mutex
	var sent [][]api.Message
	modelSystem := ""
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			json.NewEncoder(w).Encode(api.ShowResponse{System: modelSystem})
		case "/api/chat":
			var req api.ChatRequest
			json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			sent = append(sent, req.Messages)
			mu.Unlock()
			json.NewEncoder(w).Encode(api.ChatResponse{Message: api.Message{Role: "assistant", Content: "ok"}, Done: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()
	t.Setenv("OLLAMA_HOST", ollama.URL)

	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}, Dev: true}
	t.Cleanup(func() { s.Store.Close() })
	if err := s.Store.SetupVault(testPassword); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	post := func() []api.Message {
		t.Helper()
		body, _ := json.Marshal(responses.ChatRequest{Model: "test", Prompt: "hi"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/new", bytes.NewReader(body))
		req.SetPathValue("id", "new")
		if err := s.chat(httptest.NewRecorder(), req); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		return sent[len(sent)-1]
	}

	// Without instructions, the model's own prompt is left to the model.
	if msgs := post(); msgs[0].Role == "system" {
		t.Fatalf("sent a system prompt with no instructions: %+v", msgs[0])
	}

	const instructions = "Time has passed since your training data ends."
	if rec := request(http.MethodPut, "/api/v1/custom-instructions", `{"instructions":"`+instructions+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := request(http.MethodGet, "/api/v1/custom-instructions", ""); !strings.Contains(rec.Body.String(), instructions) {
		t.Fatalf("read back: %s", rec.Body)
	}
	if msgs := post(); msgs[0].Role != "system" || msgs[0].Content != instructions || msgs[1].Role != "user" {
		t.Fatalf("messages = %+v", msgs)
	}

	// A model's own prompt is kept, with the instructions after it.
	modelSystem = "You are a pirate."
	if msgs := post(); msgs[0].Content != "You are a pirate.\n\n"+instructions {
		t.Fatalf("system prompt = %q", msgs[0].Content)
	}

	if rec := request(http.MethodPut, "/api/v1/custom-instructions", `{"instructions":"`+strings.Repeat("x", store.MaxCustomInstructions+1)+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("too long: %d", rec.Code)
	}
	request(http.MethodPost, "/api/v1/vault/lock", "")
	if rec := request(http.MethodGet, "/api/v1/custom-instructions", ""); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), instructions) {
		t.Fatalf("read while locked: %d %s", rec.Code, rec.Body)
	}
}
