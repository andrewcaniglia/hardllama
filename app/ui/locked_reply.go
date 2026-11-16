//go:build windows || darwin

package ui

import (
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/app/store"
)

// A reply keeps going if chats lock while it's being written. The window
// is told the reply is done and sent nothing more, and the rest of the reply
// is encrypted as it arrives, so it can't be read until the next unlock,
// when it's added to the chat. Private chats are wiped on lock, so their
// replies stop as before.

// errToolWhileLocked stops a reply that wants to use a tool after chats
// locked. Tools need to read the chat.
var errToolWhileLocked = errors.New("the reply stopped at a tool while chats were locked")

type lockedReply struct {
	store   *store.Store
	chatID  string
	model   string
	private bool
	// onLock runs once, when chats lock during the reply.
	onLock func()
	// thinkingAtLock reports whether the model was thinking when chats
	// locked.
	thinkingAtLock func() bool

	active atomic.Bool
	// thinking is whether the model is thinking, once chats have locked,
	// so the reply records when it stopped.
	thinking bool
}

func (l *lockedReply) locked() bool {
	_, locked, err := l.store.VaultStatus()
	return err == nil && locked
}

// wrap passes each piece of the reply to next until chats lock, then keeps
// it encrypted instead.
func (l *lockedReply) wrap(next func(api.ChatResponse) error) func(api.ChatResponse) error {
	return func(res api.ChatResponse) error {
		if l.private && l.locked() {
			// Private chats were wiped. Stop, and send the window nothing.
			return store.ErrVaultLocked
		}
		if !l.active.Load() {
			if l.private || !l.locked() {
				err := next(res)
				if l.private || !errors.Is(err, store.ErrVaultLocked) {
					return err
				}
				// Chats locked while this piece was being saved, so it
				// wasn't.
			}
			l.active.Store(true)
			l.thinking = l.thinkingAtLock != nil && l.thinkingAtLock()
			l.onLock()
		}
		if len(res.Message.ToolCalls) > 0 {
			return errToolWhileLocked
		}
		piece := store.ReplyPiece{Content: res.Message.Content, Thinking: res.Message.Thinking}
		now := time.Now()
		if piece.Thinking != "" && !l.thinking {
			piece.ThinkingStart = &now
			l.thinking = true
		}
		if piece.Content != "" && l.thinking {
			piece.ThinkingEnd = &now
			l.thinking = false
		}
		if piece.Content == "" && piece.Thinking == "" && piece.ThinkingEnd == nil {
			return nil
		}
		return l.store.ContinueReply(l.chatID, l.model, piece)
	}
}

// finish records the end of the reply.
func (l *lockedReply) finish() {
	if l.thinking {
		// It stopped while thinking.
		now := time.Now()
		if err := l.store.ContinueReply(l.chatID, l.model, store.ReplyPiece{ThinkingEnd: &now}); err != nil {
			slog.Warn("failed to record the end of thinking", "error", err)
		}
		l.thinking = false
	}
	l.store.FinishReply(l.chatID)
}

// getReplyFinishing reports whether a chat's reply is still being written
// after chats locked, so the window can show it as it finishes.
func (s *Server) getReplyFinishing(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, http.StatusOK, map[string]bool{"finishing": s.Store.ReplyFinishing(r.PathValue("id"))})
}
