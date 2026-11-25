package store

import (
	"database/sql"
	"errors"
)

// FolderSnapshot returns the document snapshot associated with a saved chat.
func (s *Store) FolderSnapshot(id string) (string, error) {
	if err := s.chatDB(); err != nil {
		return "", err
	}
	s.private.mu.Lock()
	if _, ok := s.private.chats[id]; ok {
		value := s.private.folders[id]
		s.private.mu.Unlock()
		return value, nil
	}
	s.private.mu.Unlock()
	var value string
	err := s.db.conn.QueryRow("SELECT snapshot FROM chat_folders WHERE chat_id=?", id).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s.db.open("snapshot", value)
}

func (s *Store) SetFolderSnapshot(id, value string) error {
	if err := s.chatDB(); err != nil {
		return err
	}
	s.private.mu.Lock()
	if chat, ok := s.private.chats[id]; ok {
		if chat != nil {
			if s.private.folders == nil {
				s.private.folders = make(map[string]string)
			}
			s.private.folders[id] = value
		}
		s.private.mu.Unlock()
		return nil
	}
	s.private.mu.Unlock()
	tx, err := s.db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.db.writeFolder(tx, id, value); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *database) writeFolder(tx *sql.Tx, id, value string) error {
	if value == "" {
		_, err := tx.Exec("DELETE FROM chat_folders WHERE chat_id=?", id)
		return err
	}
	sealed, err := db.seal("snapshot", value)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO chat_folders(chat_id,snapshot) VALUES(?,?)
 ON CONFLICT(chat_id) DO UPDATE SET snapshot=excluded.snapshot`, id, sealed)
	return err
}
