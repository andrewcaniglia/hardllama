//go:build windows || darwin

package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// Existing tests exercise the updater itself, which this build switches off.
func TestMain(m *testing.M) {
	Disabled = false
	os.Exit(m.Run())
}

func disableUpdates(t *testing.T) {
	t.Helper()

	Disabled = true
	t.Cleanup(func() { Disabled = false })
}

func TestDisabledMakesNoRequests(t *testing.T) {
	disableUpdates(t)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	previousURL, previousDelay := UpdateCheckURLBase, UpdateCheckInitialDelay
	UpdateCheckURLBase, UpdateCheckInitialDelay = server.URL, time.Millisecond
	t.Cleanup(func() { UpdateCheckURLBase, UpdateCheckInitialDelay = previousURL, previousDelay })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	u := &Updater{}
	done := u.startBackgroundUpdaterChecker(ctx, func(string) error {
		t.Error("update callback called")
		return nil
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("update checker kept running")
	}
	u.TriggerImmediateCheck()
	time.Sleep(20 * time.Millisecond)

	if got := requests.Load(); got != 0 {
		t.Fatalf("expected no requests, got %d", got)
	}
}

func TestDisabledRefusesDownload(t *testing.T) {
	disableUpdates(t)

	u := &Updater{}
	if err := u.DownloadNewRelease(t.Context(), UpdateResponse{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

// An update staged before updates were disabled must stay inert.
func TestDisabledIgnoresStagedUpdate(t *testing.T) {
	previous := UpdateStageDir
	UpdateStageDir = t.TempDir()
	t.Cleanup(func() { UpdateStageDir = previous })

	dir := filepath.Join(UpdateStageDir, "etag")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Ollama-darwin.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsUpdatePending() {
		t.Skip("staged file not recognized on this platform")
	}

	disableUpdates(t)
	if IsUpdatePending() {
		t.Fatal("expected staged update to be ignored")
	}
	if err := DoUpgrade(true); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
}
