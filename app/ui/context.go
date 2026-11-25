//go:build windows || darwin

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/store"
	"github.com/ollama/ollama/app/ui/responses"
)

// The context counter shows how much of the model's context window a chat
// uses. The engine counts exactly, but only after a reply, so text that
// hasn't been sent yet is estimated.

// imageTokens is what the engine counts each image as when deciding what
// fits.
const imageTokens = 768

// estimateTokens guesses how many tokens text is: about 4 characters each
// for English, and leaning high with one each for other characters, which
// often take more.
func estimateTokens(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if r < utf8.RuneSelf {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + other
}

// runningContextLength is the context length the model is loaded with, or 0
// if it isn't known.
func runningContextLength(ctx context.Context, c *api.Client, model string) int {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	running, err := c.ListRunning(ctx)
	if err != nil {
		return 0
	}
	for _, m := range running.Models {
		if m.Name == model || m.Model == model || m.Name == model+":latest" {
			return m.ContextLength
		}
	}
	return 0
}

// modelContextLength returns the most context a model supports, or 0 if
// unknown. The engine caps the context length setting at this.
func modelContextLength(info map[string]any) int {
	for key, value := range info {
		if !strings.HasSuffix(key, ".context_length") {
			continue
		}
		switch n := value.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

func (s *Server) getModelContextLength(w http.ResponseWriter, r *http.Request) error {
	model := r.URL.Query().Get("model")
	if model == "" {
		return writeError(w, http.StatusBadRequest, errors.New("model is required"))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	show, err := s.inferenceClient().Show(ctx, &api.ShowRequest{Model: model})
	if err != nil {
		// Not downloaded yet, or the engine isn't running.
		return writeJSON(w, http.StatusOK, responses.ModelContextLengthResponse{})
	}
	return writeJSON(w, http.StatusOK, responses.ModelContextLengthResponse{Max: modelContextLength(show.ModelInfo)})
}

func (s *Server) getContextUsage(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	u, err := s.Store.ContextUsage(id)
	if err != nil {
		return storeError(w, err)
	}
	resp := responses.ContextUsageResponse{Usage: u}
	if chat, err := s.Store.Chat(id); err == nil {
		estimateChat(chat, &resp)
	}
	return writeJSON(w, http.StatusOK, resp)
}

// estimateChat guesses the tokens in a chat as it would be sent, for when
// the engine hasn't counted it with the current model.
func estimateChat(chat *store.Chat, resp *responses.ContextUsageResponse) {
	for _, m := range chat.Messages {
		resp.Estimate += estimateTokens(m.Content) + estimateTokens(m.Thinking)
		if m.Role != "user" {
			continue
		}
		for _, a := range m.Attachments {
			resp.AttachmentCount++
			resp.AttachmentTokens += estimateAttachment(a.Filename, a.Data)
		}
	}
	resp.Estimate += resp.AttachmentTokens
}

func estimateAttachment(filename string, data []byte) int {
	if isImageAttachment(filename) {
		return imageTokens
	}
	// Matches how buildChatRequest adds the file to the message.
	text := convertBytesToText(data, filename)
	return estimateTokens(fmt.Sprintf("\n--- File: %s ---\n%s\n--- End of %s ---", filename, text, filename))
}

// estimateAttachments estimates attachments the way they'll be sent: images
// as the engine counts them, and other files as the text extracted from
// them.
func (s *Server) estimateAttachments(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Attachments []struct {
			Filename string `json:"filename"`
			Data     []byte `json:"data"`
		} `json:"attachments"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	resp := responses.AttachmentEstimateResponse{Attachments: []responses.AttachmentEstimate{}}
	for _, a := range req.Attachments {
		tokens := estimateAttachment(a.Filename, a.Data)
		resp.Attachments = append(resp.Attachments, responses.AttachmentEstimate{
			Filename: a.Filename,
			Tokens:   tokens,
			Image:    isImageAttachment(a.Filename),
		})
	}
	return writeJSON(w, http.StatusOK, resp)
}
