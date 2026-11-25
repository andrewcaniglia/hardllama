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

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/share"
	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/app/ui/responses"
)

func TestEstimateTokens(t *testing.T) {
	for _, tt := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 2},
		{"héllo", 2}, // 4 ASCII and one other
		{"日本語", 3},   // one each
		{strings.Repeat("a", 4000), 1000},
	} {
		if got := estimateTokens(tt.text); got != tt.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestChatRecordsContextUsage(t *testing.T) {
	// The engine reports how many tokens it was sent. The second reply was
	// sent fewer, because the chat no longer fit.
	prompts := []int{3000, 2500}
	turn := 0
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			json.NewEncoder(w).Encode(api.ShowResponse{})
		case "/api/ps":
			json.NewEncoder(w).Encode(api.ProcessResponse{Models: []api.ProcessModelResponse{
				{Name: "other:latest", Model: "other:latest", ContextLength: 1},
				{Name: "test:latest", Model: "test:latest", ContextLength: 4096},
			}})
		case "/api/chat":
			res := api.ChatResponse{
				Message: api.Message{Role: "assistant", Content: "reply"},
				Done:    true,
			}
			res.PromptEvalCount = prompts[turn]
			res.EvalCount = 200
			turn++
			json.NewEncoder(w).Encode(res)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()
	t.Setenv("OLLAMA_HOST", ollama.URL)

	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}, Dev: true}
	t.Cleanup(func() { s.Store.Close() })
	if err := s.Store.SetSettings(store.Settings{ContextLength: 4096}); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()

	post := func(id string) string {
		t.Helper()
		body, _ := json.Marshal(responses.ChatRequest{Model: "test", Prompt: "hello there"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/"+id, bytes.NewReader(body))
		req.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		if err := s.chat(rec, req); err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(rec.Body)
		for {
			var event responses.ChatEvent
			if err := dec.Decode(&event); err != nil {
				return id
			}
			if event.EventName == "chat_created" {
				id = *event.ChatID
			}
		}
	}
	getContext := func(id string) responses.ContextUsageResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/chat/"+id+"/context", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("context: %d %s", rec.Code, rec.Body)
		}
		var resp responses.ContextUsageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	id := post("new")
	got := getContext(id)
	want := store.ContextUsage{Model: "test", Prompt: 3000, Reply: 200, Limit: 4096, Configured: 4096, Messages: 1}
	if got.Usage == nil || *got.Usage != want {
		t.Fatalf("usage = %+v, want %+v", got.Usage, want)
	}
	if got.Estimate == 0 {
		t.Fatal("no estimate for the chat")
	}

	post(id)
	got = getContext(id)
	if got.Usage == nil || got.Usage.Prompt != 2500 || got.Usage.Messages != 3 || !got.Usage.Truncated {
		t.Fatalf("expected the shorter prompt to show the chat was cut: %+v", got.Usage)
	}
}

func TestSavedAttachmentBreakdown(t *testing.T) {
	chat := store.NewChat("counter")
	data := []byte(strings.Repeat("a", 51*1024))
	chat.Messages = append(chat.Messages, store.NewMessage("user", "abcd", &store.MessageOptions{
		Attachments: []store.File{{Filename: "notes.txt", Data: data}},
	}))
	var resp responses.ContextUsageResponse
	estimateChat(chat, &resp)
	if resp.AttachmentCount != 1 || resp.AttachmentTokens < 13056 || resp.Estimate != resp.AttachmentTokens+1 {
		t.Fatalf("unexpected attachment estimate: %+v", resp)
	}
}

func TestEstimateAttachmentsRoute(t *testing.T) {
	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}, Dev: true}
	t.Cleanup(func() { s.Store.Close() })

	body, _ := json.Marshal(map[string]any{"attachments": []map[string]any{
		{"filename": "notes.txt", "data": []byte(strings.Repeat("a", 400))},
		{"filename": "photo.PNG", "data": []byte{0x89, 'P', 'N', 'G'}},
	}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/estimate-attachments", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var resp responses.AttachmentEstimateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Attachments) != 2 {
		t.Fatalf("%+v", resp)
	}
	// 100 for the text, plus the file markers it's sent with.
	if text := resp.Attachments[0]; text.Image || text.Tokens < 100 || text.Tokens > 120 {
		t.Errorf("text file: %+v", text)
	}
	if img := resp.Attachments[1]; !img.Image || img.Tokens != imageTokens {
		t.Errorf("image: %+v", img)
	}
}

func TestContextLengthSentToPairedComputer(t *testing.T) {
	st := &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}
	t.Cleanup(func() { st.Close() })
	if err := st.SetupVault(testPassword); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st}
	chat := store.NewChat("c1")
	chat.Messages = append(chat.Messages, store.NewMessage("user", "hi", nil))

	numCtx := func() any {
		t.Helper()
		req, err := s.buildChatRequest(chat, "test", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return req.Options["num_ctx"]
	}
	setLength := func(n int) {
		t.Helper()
		settings, _ := st.Settings()
		settings.ContextLength = n
		if err := st.SetSettings(settings); err != nil {
			t.Fatal(err)
		}
	}

	// This computer's engine is started with the setting.
	setLength(16384)
	if got := numCtx(); got != nil {
		t.Fatalf("sent num_ctx %v to this computer", got)
	}

	if err := st.SetModelServer(&share.Pairing{Address: "192.168.1.20:11443", Fingerprint: "f", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if got := numCtx(); got != 16384 {
		t.Fatalf("paired computer got num_ctx %v, want 16384", got)
	}
	// The default is left to the other computer.
	setLength(0)
	if got := numCtx(); got != nil {
		t.Fatalf("sent num_ctx %v with no setting", got)
	}
}

func TestModelContextLengthRoute(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req api.ShowRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "small" {
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(api.ShowResponse{ModelInfo: map[string]any{
			"general.architecture":   "llama",
			"llama.context_length":   8192,
			"llama.embedding_length": 4096,
		}})
	}))
	defer ollama.Close()
	t.Setenv("OLLAMA_HOST", ollama.URL)

	s := &Server{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}, Dev: true}
	t.Cleanup(func() { s.Store.Close() })
	get := func(model string) string {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/model-context-length?model="+model, nil))
		return strings.TrimSpace(rec.Body.String())
	}
	if got := get("small"); got != `{"max":8192}` {
		t.Fatalf("small = %s", got)
	}
	if got := get("missing"); got != `{"max":0}` {
		t.Fatalf("missing model = %s", got)
	}
}
