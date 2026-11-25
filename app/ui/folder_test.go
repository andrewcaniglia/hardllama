//go:build windows || darwin

package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/store"
)

func TestFolderExcerptsAreRequestOnly(t *testing.T) {
	const excerpt = "Source passage from the folder"
	var prompts []string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			var request api.ChatRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			var content strings.Builder
			for _, message := range request.Messages {
				content.WriteString(message.Content)
			}
			prompts = append(prompts, content.String())
			json.NewEncoder(w).Encode(api.ChatResponse{Done: true, Message: api.Message{Role: "assistant", Content: "OK"}})
		default:
			w.Write([]byte("{}"))
		}
	}))
	defer engine.Close()
	t.Setenv("OLLAMA_HOST", engine.URL)
	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}}
	defer s.Store.Close()
	id := "new"
	for _, body := range []string{
		`{"model":"test","prompt":"Question","folderContext":"Source passage from the folder","folderSnapshot":{"name":"handbook","files":1,"skipped":0,"passages":[{"filename":"guide.md","text":"A separate historical fact"}]}}`,
		`{"model":"test","prompt":"Follow-up"}`,
	} {
		request := httptest.NewRequest("POST", "/api/v1/chat/test", strings.NewReader(body))
		request.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		if err := s.chat(rec, request); err != nil {
			t.Fatal(err)
		}
		if id == "new" {
			var created struct {
				ChatID string `json:"chatId"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&created); err != nil || created.ChatID == "" {
				t.Fatal("missing chat creation event", err)
			}
			id = created.ChatID
		}
	}
	folderRequest := httptest.NewRequest("GET", "/api/v1/chat/"+id+"/folder", nil)
	folderRequest.SetPathValue("id", id)
	saved := httptest.NewRecorder()
	if err := s.chatFolder(saved, folderRequest); err != nil || !strings.Contains(saved.Body.String(), "A separate historical fact") {
		t.Fatal("missing saved snapshot", err)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[0], excerpt) || strings.Contains(prompts[1], excerpt) || strings.Contains(prompts[0], "A separate historical fact") {
		t.Fatalf("unexpected inference prompts: %q", prompts)
	}
}
