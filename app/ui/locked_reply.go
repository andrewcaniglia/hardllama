//go:build windows || darwin

package ui

import (
	"errors"
	"net/http"
	"sync/atomic"

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

	active atomic.Bool
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
			l.onLock()
		}
		if len(res.Message.ToolCalls) > 0 {
			return errToolWhileLocked
		}
		if res.Message.Content == "" && res.Message.Thinking == "" {
			return nil
		}
		return l.store.ContinueReply(l.chatID, l.model, res.Message.Content, res.Message.Thinking)
	}
}

// getReplyFinishing reports whether a chat's reply is still being written
// after chats locked, so the window can show it as it finishes.
func (s *Server) getReplyFinishing(w http.ResponseWriter, r *http.Request) error {
	return writeJSON(w, http.StatusOK, map[string]bool{"finishing": s.Store.ReplyFinishing(r.PathValue("id"))})
}
