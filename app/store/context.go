package store

import (
	"encoding/json"
)

// ContextUsage is how much of the model's context window the last reply in
// a chat used, as counted by the model engine.
type ContextUsage struct {
	Model string `json:"model"`
	// Prompt is every token the model was sent: the chat so far,
	// attachments, the system prompt and the model's formatting.
	Prompt int `json:"prompt"`
	Reply  int `json:"reply"`
	// Limit is the context length the model ran with, or 0 if unknown.
	Limit int `json:"limit"`
	// Configured is the context length setting at the time, 0 for the
	// default. If it has changed since, Limit is out of date.
	Configured int `json:"configured"`
	// Messages is how many messages the chat had when it was sent.
	Messages int `json:"messages"`
	// Truncated means the model couldn't see the whole chat.
	Truncated bool `json:"truncated"`
}

// ContextUsage returns the usage recorded for a chat's last reply, or nil.
func (s *Store) ContextUsage(chatID string) (*ContextUsage, error) {
	if _, ok := s.private.get(chatID); ok {
		return s.private.contextUsage(chatID), nil
	}
	if err := s.chatDB(); err != nil {
		return nil, err
	}
	value, err := s.db.getContextUsage(chatID)
	if err != nil || value == "" {
		return nil, err
	}
	var u ContextUsage
	if err := json.Unmarshal([]byte(value), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// RecordContextUsage saves the usage of a chat's latest reply, working out
// whether the model could see the whole chat.
func (s *Store) RecordContextUsage(chatID string, u ContextUsage) error {
	prev, err := s.ContextUsage(chatID)
	if err != nil {
		return err
	}
	u.Truncated = wasTruncated(prev, u)

	if _, ok := s.private.get(chatID); ok {
		s.private.setContextUsage(chatID, u)
		return nil
	}
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	if err := s.chatDB(); err != nil {
		return err
	}
	return s.db.setContextUsage(chatID, string(data))
}

// wasTruncated reports whether the model engine left part of the chat out.
// It drops the oldest messages when a chat doesn't fit, and the start of
// the reply itself if the reply runs past the limit.
func wasTruncated(prev *ContextUsage, u ContextUsage) bool {
	if u.Limit > 0 && u.Prompt+u.Reply > u.Limit {
		return true
	}
	// A chat only grows, so if the model was sent less than last time,
	// older messages were left out.
	return prev != nil && prev.Model == u.Model && u.Messages > prev.Messages && u.Prompt < prev.Prompt
}
