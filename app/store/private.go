package store

import (
	"encoding/json"
	"errors"
	"slices"
	"sync"
)

// privateChats holds chats that are never written to disk. They exist only for
// the lifetime of the process.
//
// IDs stay in the map after deletion with a nil value. A response can still be
// streaming when its chat is deleted, and those late writes must be dropped
// rather than falling through to the database.
type privateChats struct {
	mu    sync.Mutex
	chats map[string]*Chat
}

// get reports whether id is private. The chat is nil if it has been deleted.
func (p *privateChats) get(id string) (*Chat, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	chat, ok := p.chats[id]
	if !ok {
		return nil, false
	}
	if chat == nil {
		return nil, true
	}
	return cloneChat(chat), true
}

// set stores chat and reports whether it is private. Chats that are not
// already known are only added if they are marked private.
func (p *privateChats) set(chat Chat) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing, ok := p.chats[chat.ID]
	if !ok && !chat.Private {
		return false
	}
	if ok && existing == nil {
		return true
	}

	if p.chats == nil {
		p.chats = make(map[string]*Chat)
	}
	chat.Private = true
	p.chats[chat.ID] = cloneChat(&chat)
	return true
}

// update applies fn to the chat with id and reports whether id is private.
func (p *privateChats) update(id string, fn func(*Chat) error) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	chat, ok := p.chats[id]
	if !ok {
		return false, nil
	}
	if chat == nil {
		return true, nil
	}
	return true, fn(chat)
}

// remove deletes the chat with id and reports whether id is private.
func (p *privateChats) remove(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.chats[id]; !ok {
		return false
	}
	p.chats[id] = nil
	return true
}

// clear deletes every private chat.
func (p *privateChats) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id := range p.chats {
		p.chats[id] = nil
	}
}

// list returns all private chats, newest first.
func (p *privateChats) list() []Chat {
	p.mu.Lock()
	defer p.mu.Unlock()

	chats := make([]Chat, 0, len(p.chats))
	for _, chat := range p.chats {
		if chat != nil {
			chats = append(chats, *cloneChat(chat))
		}
	}
	slices.SortFunc(chats, func(a, b Chat) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return chats
}

func cloneChat(chat *Chat) *Chat {
	c := *chat
	c.Messages = slices.Clone(chat.Messages)
	for i := range c.Messages {
		c.Messages[i].Attachments = slices.Clone(c.Messages[i].Attachments)
		c.Messages[i].ToolCalls = slices.Clone(c.Messages[i].ToolCalls)
	}
	c.BrowserState = slices.Clone(chat.BrowserState)
	return &c
}

// updateLastMessage mirrors database.updateLastMessage, which only replaces
// the fields that change while a response streams.
func updateLastMessage(chat *Chat, msg Message) error {
	if len(chat.Messages) == 0 {
		return errors.New("no message to update")
	}

	last := &chat.Messages[len(chat.Messages)-1]
	last.Content = msg.Content
	last.Thinking = msg.Thinking
	last.Model = msg.Model
	last.UpdatedAt = msg.UpdatedAt
	last.ThinkingTimeStart = msg.ThinkingTimeStart
	last.ThinkingTimeEnd = msg.ThinkingTimeEnd
	last.ToolResult = msg.ToolResult
	return nil
}

func setBrowserState(chat *Chat, state json.RawMessage) {
	chat.BrowserState = slices.Clone(state)
}
