package store

import (
	"crypto/hpke"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// A reply can keep going after chats lock. Its key is gone, so the rest of
// the reply is encrypted to the same public key as drafts, a piece at a time
// as it arrives, and only held in memory. It's added to the chat at the next
// unlock. If HardLlama quits first, it's lost.

const replyInfo = "hardllama-reply-v1\x00"

type pendingReply struct {
	model string
	// messages is how many messages the chat had when the reply was cut
	// off. If that has changed by the time it's added, it's dropped
	// rather than added to the wrong message.
	messages int
	enc      []byte
	sender   *hpke.Sender
	pieces   [][]byte
	done     bool
}

type replyPiece struct {
	Content  string `json:"c,omitempty"`
	Thinking string `json:"t,omitempty"`
}

type pendingReplies struct {
	mu      sync.Mutex
	replies map[string]*pendingReply
}

// ContinueReply adds the next piece of a reply that's still being written
// after chats locked. While locked, it's encrypted so only the password can
// read it, and added to the chat at the next unlock. While unlocked, it's
// added right away.
func (s *Store) ContinueReply(chatID, model, content, thinking string) error {
	if err := s.ensureDB(); err != nil {
		return err
	}
	s.pending.mu.Lock()
	defer s.pending.mu.Unlock()

	p := s.pending.replies[chatID]
	if p == nil {
		// Counting messages doesn't need the key.
		n, err := s.db.messageCount(chatID)
		if err != nil {
			return err
		}
		p = &pendingReply{model: model, messages: n}
		if s.pending.replies == nil {
			s.pending.replies = map[string]*pendingReply{}
		}
		s.pending.replies[chatID] = p
	}

	if !s.db.vaultLocked() {
		if err := s.addReplyLocked(chatID, p); err != nil {
			return err
		}
		return s.applyReplyLocked(chatID, p, replyPiece{Content: content, Thinking: thinking})
	}

	if p.sender == nil {
		s.drafts.mu.Lock()
		publicKey := s.drafts.publicKey
		s.drafts.mu.Unlock()
		if publicKey == nil {
			return ErrVaultLocked
		}
		enc, sender, err := hpke.NewSender(publicKey, draftKDF, draftAEAD, []byte(replyInfo+chatID))
		if err != nil {
			return err
		}
		p.enc, p.sender = enc, sender
	}
	data, err := json.Marshal(replyPiece{Content: content, Thinking: thinking})
	if err != nil {
		return err
	}
	sealed, err := p.sender.Seal(nil, data)
	if err != nil {
		return err
	}
	p.pieces = append(p.pieces, sealed)
	return nil
}

// FinishReply marks a reply that kept going after chats locked as done.
func (s *Store) FinishReply(chatID string) {
	s.pending.mu.Lock()
	defer s.pending.mu.Unlock()
	if p := s.pending.replies[chatID]; p != nil {
		p.done = true
		if len(p.pieces) == 0 {
			delete(s.pending.replies, chatID)
		}
	}
}

// ReplyFinishing reports whether a chat's reply is still being written
// after chats locked.
func (s *Store) ReplyFinishing(chatID string) bool {
	s.pending.mu.Lock()
	defer s.pending.mu.Unlock()
	p := s.pending.replies[chatID]
	return p != nil && !p.done
}

// addReplies adds the parts of replies written while locked to their chats.
func (s *Store) addReplies() {
	s.pending.mu.Lock()
	defer s.pending.mu.Unlock()
	for chatID, p := range s.pending.replies {
		if err := s.addReplyLocked(chatID, p); err != nil {
			slog.Warn("failed to add the rest of a reply", "error", err)
			delete(s.pending.replies, chatID)
			continue
		}
		if p.done {
			delete(s.pending.replies, chatID)
		}
	}
}

// addReplyLocked decrypts what was written while locked and adds it to the
// chat. s.pending.mu must be held, and chats unlocked.
func (s *Store) addReplyLocked(chatID string, p *pendingReply) error {
	if len(p.pieces) == 0 {
		return nil
	}
	private, err := s.drafts.privateKey(s.db.vault.Load())
	if err != nil {
		return err
	}
	if private == nil {
		return errors.New("no key for the rest of the reply")
	}
	key, err := draftKEM.NewPrivateKey(private)
	if err != nil {
		return err
	}
	r, err := hpke.NewRecipient(p.enc, key, draftKDF, draftAEAD, []byte(replyInfo+chatID))
	if err != nil {
		return err
	}
	var all replyPiece
	for _, sealed := range p.pieces {
		data, err := r.Open(nil, sealed)
		if err != nil {
			return ErrVaultDecryptFail
		}
		var piece replyPiece
		if err := json.Unmarshal(data, &piece); err != nil {
			return err
		}
		all.Content += piece.Content
		all.Thinking += piece.Thinking
	}
	// The next time chats lock starts a new sender.
	p.enc, p.sender, p.pieces = nil, nil, nil
	return s.applyReplyLocked(chatID, p, all)
}

// applyReplyLocked adds text to the reply in the chat. s.pending.mu must be
// held, and chats unlocked.
func (s *Store) applyReplyLocked(chatID string, p *pendingReply, piece replyPiece) error {
	if piece.Content == "" && piece.Thinking == "" {
		return nil
	}
	chat, err := s.Chat(chatID)
	if err != nil {
		return err
	}
	if len(chat.Messages) != p.messages {
		return errors.New("the chat changed before the rest of the reply could be added")
	}
	if n := len(chat.Messages); n > 0 && chat.Messages[n-1].Role == "assistant" {
		msg := chat.Messages[n-1]
		msg.Content += piece.Content
		msg.Thinking += piece.Thinking
		msg.UpdatedAt = time.Now()
		return s.UpdateLastMessage(chatID, msg)
	}
	// The reply hadn't started when chats locked.
	msg := NewMessage("assistant", piece.Content, &MessageOptions{Model: p.model, Thinking: piece.Thinking})
	if err := s.AppendMessage(chatID, msg); err != nil {
		return err
	}
	p.messages++
	return nil
}
