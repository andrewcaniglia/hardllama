//go:build windows || darwin

package main

import (
	"log/slog"
	"sync"
	"time"
)

// Chats lock a chosen time after the user leaves HardLlama, by switching to
// another app or closing the window, unless they come back first.

// afterFunc is replaced in tests.
var afterFunc = time.AfterFunc

var leaving struct {
	mu    sync.Mutex
	timer *time.Timer
}

// leftApp starts counting down to locking chats. Leaving again while
// already counting doesn't restart it.
func leftApp() {
	if appStore == nil {
		return
	}
	delay, err := appStore.LockDelay()
	if err != nil {
		slog.Warn("failed to read lock delay, using the shortest", "error", err)
	}
	if delay == 0 {
		returnedToApp()
		appStore.LockVault()
		wv.showLockScreen()
		return
	}
	leaving.mu.Lock()
	defer leaving.mu.Unlock()
	if leaving.timer != nil {
		return
	}
	leaving.timer = afterFunc(delay, func() {
		leaving.mu.Lock()
		leaving.timer = nil
		leaving.mu.Unlock()
		appStore.LockVault()
		wv.showLockScreen()
	})
}

// returnedToApp keeps chats unlocked if the user came back in time.
func returnedToApp() {
	leaving.mu.Lock()
	defer leaving.mu.Unlock()
	if leaving.timer != nil {
		leaving.timer.Stop()
		leaving.timer = nil
	}
}
