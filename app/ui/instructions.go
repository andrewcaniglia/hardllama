//go:build windows || darwin

package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ollama/ollama/app/store"
)

// systemPrompt is the system prompt for a chat: the model's own, followed by
// the user's custom instructions. A system prompt sent with a chat replaces
// the model's, so it's kept by including it.
func (s *Server) systemPrompt(modelSystem string) (string, error) {
	instructions, err := s.Store.CustomInstructions()
	if err != nil {
		return "", err
	}
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		// Leave the model's own prompt to the model.
		return "", nil
	}
	if modelSystem = strings.TrimSpace(modelSystem); modelSystem != "" {
		return modelSystem + "\n\n" + instructions, nil
	}
	return instructions, nil
}

type customInstructions struct {
	Instructions string `json:"instructions"`
}

func (s *Server) getCustomInstructions(w http.ResponseWriter, r *http.Request) error {
	value, err := s.Store.CustomInstructions()
	if err != nil {
		return storeError(w, err)
	}
	return writeJSON(w, http.StatusOK, customInstructions{Instructions: value})
}

func (s *Server) setCustomInstructions(w http.ResponseWriter, r *http.Request) error {
	var req customInstructions
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if err := s.Store.SetCustomInstructions(req.Instructions); err != nil {
		if errors.Is(err, store.ErrCustomInstructionsTooLong) {
			return writeError(w, http.StatusBadRequest, err)
		}
		return storeError(w, err)
	}
	return writeJSON(w, http.StatusOK, req)
}
