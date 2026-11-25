//go:build darwin || windows

package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ollama/ollama/app/dialog"
	"github.com/ollama/ollama/app/store"
)

func (w *Webview) saveChatExport(format string, data []byte) (bool, error) {
	switch format {
	case "pdf", "md", "txt":
	default:
		return false, errors.New("unsupported chat export format")
	}
	unlocked := func() bool {
		if w.Store == nil {
			return false
		}
		enabled, locked, err := w.Store.VaultStatus()
		return err == nil && enabled && !locked
	}
	if !unlocked() {
		return false, store.ErrVaultLocked
	}
	path, err := dialog.File().Title("Export chat").Filter("Chat transcript", format).SetStartFile("Chat." + format).Save()
	if errors.Is(err, dialog.ErrCancelled) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("could not open the save dialog")
	}
	if !unlocked() {
		return false, store.ErrVaultLocked
	}

	// Complete the private, mode-0600 file before replacing the destination.
	file, err := os.CreateTemp(filepath.Dir(path), ".hardllama-export-*")
	if err != nil {
		return false, errors.New("could not create the export file")
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return false, errors.New("could not write the export file")
	}
	if !unlocked() {
		return false, store.ErrVaultLocked
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return false, errors.New("could not save the export at the selected location")
	}
	return true, nil
}
