//go:build darwin

package main

import "testing"

// HardLlama never starts the Claude Desktop gateway or changes Claude's
// settings, even if Claude Desktop is installed and was connected by the
// official app.
func TestNoThirdPartyApps(t *testing.T) {
	if thirdPartyAppsEnabled {
		t.Fatal("other companies' apps are turned on")
	}

	previousInstalled, previousDesktop := claudeDesktopInstalled, claudeDesktop
	claudeDesktopInstalled = func() bool { return true }
	fake := &fakeClaudeDesktopController{configured: true}
	claudeDesktop = fake
	t.Cleanup(func() {
		claudeDesktopInstalled, claudeDesktop = previousInstalled, previousDesktop
	})

	if err := reconcileClaudeAppProxy(); err != nil {
		t.Fatal(err)
	}
	claudeProxyMu.Lock()
	running := claudeAppProxy != nil
	claudeProxyMu.Unlock()
	if running {
		t.Fatal("Claude gateway started")
	}
	if fake.configureCalls != 0 {
		t.Fatal("Claude Desktop's settings were changed")
	}
}
