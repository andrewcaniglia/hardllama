//go:build windows || darwin

package ui

import (
	"encoding/json"
	"net/http"
)

func (s *Server) chatFolder(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if ok, err := s.requireUnlocked(w); !ok {
		return err
	}
	if r.Method == http.MethodPut {
		var snapshot json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 192<<20)).Decode(&snapshot); err != nil {
			return writeError(w, 400, err)
		}
		value := string(snapshot)
		if value == "null" {
			value = ""
		}
		if err := s.Store.SetFolderSnapshot(id, value); err != nil {
			return storeError(w, err)
		}
		return writeJSON(w, 200, nil)
	}
	value, err := s.Store.FolderSnapshot(id)
	if err != nil {
		return storeError(w, err)
	}
	if value == "" {
		return writeJSON(w, 200, nil)
	}
	return writeJSON(w, 200, json.RawMessage(value))
}
