//go:build windows || darwin

package store

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	vaultDetailSecret = "sentinel-5e7d-detail"
	// vaultDetailDate is a day that appears nowhere else in the database.
	vaultDetailDate = "1987-06-05"
)

var vaultDetailTime = time.Date(1987, 6, 5, 4, 3, 2, 1000, time.UTC)

// assertAllSealed checks the database directly: every non-empty value in an
// encrypted column must be encrypted.
func assertAllSealed(t *testing.T, dbPath string) {
	t.Helper()

	raw, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	for _, col := range sealedColumns {
		var total, plain int
		query := fmt.Sprintf(
			`SELECT COUNT(*), COALESCE(SUM(CAST(substr(%[1]s, 1, %[3]d) AS TEXT) != ?), 0) FROM %[2]s WHERE %[1]s IS NOT NULL AND length(%[1]s) > 0`,
			col.column, col.table, len(sealedPrefix),
		)
		if err := raw.QueryRow(query, string(sealedPrefix)).Scan(&total, &plain); err != nil {
			t.Fatalf("%s.%s: %v", col.table, col.column, err)
		}
		if plain != 0 {
			t.Errorf("%s.%s: %d of %d values are not encrypted", col.table, col.column, plain, total)
		}
	}

	var users int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Errorf("found %d cached accounts", users)
	}
}

func assertValuesNotOnDisk(t *testing.T, dbPath string, values ...string) {
	t.Helper()

	files, err := filepath.Glob(dbPath + "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range values {
			if bytes.Contains(data, []byte(v)) {
				t.Errorf("found %q in %s", v, file)
			}
		}
	}
}

// detailChat fills every encrypted detail column with something findable.
func detailChat(id string) Chat {
	start := vaultDetailTime.Add(time.Second)
	end := vaultDetailTime.Add(2 * time.Second)

	chat := NewChat(id)
	chat.CreatedAt = vaultDetailTime
	chat.Title = "title"

	user := NewMessage("user", "question", nil)
	user.CreatedAt, user.UpdatedAt = vaultDetailTime, vaultDetailTime

	assistant := NewMessage("assistant", "answer", &MessageOptions{Model: vaultDetailSecret + "-model"})
	assistant.CreatedAt, assistant.UpdatedAt = vaultDetailTime, vaultDetailTime.Add(3*time.Second)
	assistant.ThinkingTimeStart, assistant.ThinkingTimeEnd = &start, &end
	assistant.ToolCalls = []ToolCall{{
		Type:     vaultDetailSecret + "-type",
		Function: ToolFunction{Name: vaultDetailSecret + "-tool", Arguments: "{}"},
	}}

	chat.Messages = append(chat.Messages, user, assistant)
	return *chat
}

func assertDetailChat(t *testing.T, chat *Chat) {
	t.Helper()

	if !chat.CreatedAt.Equal(vaultDetailTime) {
		t.Errorf("chat created at %v, want %v", chat.CreatedAt, vaultDetailTime)
	}
	if len(chat.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(chat.Messages))
	}
	user, assistant := chat.Messages[0], chat.Messages[1]
	if user.Role != "user" || user.Content != "question" || !user.CreatedAt.Equal(vaultDetailTime) {
		t.Errorf("user message not restored: %+v", user)
	}
	if assistant.Role != "assistant" || assistant.Model != vaultDetailSecret+"-model" {
		t.Errorf("assistant message not restored: %+v", assistant)
	}
	if !assistant.UpdatedAt.Equal(vaultDetailTime.Add(3 * time.Second)) {
		t.Errorf("assistant updated at %v", assistant.UpdatedAt)
	}
	if assistant.ThinkingTimeStart == nil || !assistant.ThinkingTimeStart.Equal(vaultDetailTime.Add(time.Second)) ||
		assistant.ThinkingTimeEnd == nil || !assistant.ThinkingTimeEnd.Equal(vaultDetailTime.Add(2*time.Second)) {
		t.Errorf("thinking times not restored: %v %v", assistant.ThinkingTimeStart, assistant.ThinkingTimeEnd)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Type != vaultDetailSecret+"-type" ||
		assistant.ToolCalls[0].Function.Name != vaultDetailSecret+"-tool" {
		t.Errorf("tool call not restored: %+v", assistant.ToolCalls)
	}
}

func setDetailSettings(t *testing.T, s *Store) {
	t.Helper()

	settings, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.SelectedModel = vaultDetailSecret + "-selected"
	settings.WorkingDir = "/" + vaultDetailSecret + "-folder"
	if err := s.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUser(User{Name: vaultDetailSecret + "-name", Email: vaultDetailSecret + "@example.com", Plan: "pro"}); err != nil {
		t.Fatal(err)
	}
}

func assertDetailSettings(t *testing.T, s *Store) {
	t.Helper()

	settings, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SelectedModel != vaultDetailSecret+"-selected" || settings.WorkingDir != "/"+vaultDetailSecret+"-folder" {
		t.Errorf("settings not restored: model %q, folder %q", settings.SelectedModel, settings.WorkingDir)
	}
}

func TestVaultEncryptsChatDetails(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			s := privateTestStore(t)
			if !existing {
				if err := s.SetupVault(vaultPassword); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetChat(detailChat("details")); err != nil {
				t.Fatal(err)
			}
			setDetailSettings(t, s)
			if existing {
				if err := s.SetupVault(vaultPassword); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			assertAllSealed(t, s.DBPath)
			assertValuesNotOnDisk(t, s.DBPath, vaultDetailSecret, vaultDetailDate)

			reopened := &Store{DBPath: s.DBPath}
			defer reopened.Close()
			if err := reopened.UnlockVault(vaultPassword); err != nil {
				t.Fatal(err)
			}
			chat, err := reopened.Chat("details")
			if err != nil {
				t.Fatal(err)
			}
			assertDetailChat(t, chat)
			assertDetailSettings(t, reopened)
		})
	}
}

// Updating the last message, as streaming does, must keep its details
// encrypted too.
func TestVaultEncryptsUpdatedMessages(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	chat := detailChat("details")
	chat.Messages = chat.Messages[:1]
	if err := s.SetChat(chat); err != nil {
		t.Fatal(err)
	}
	full := detailChat("details")
	if err := s.AppendMessage("details", NewMessage("assistant", "", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateLastMessage("details", full.Messages[1]); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	assertAllSealed(t, s.DBPath)
	assertValuesNotOnDisk(t, s.DBPath, vaultDetailSecret, vaultDetailDate)
}

// Vaults made by earlier versions only encrypted chat content. Unlocking one
// must encrypt the rest without encrypting content twice.
func TestVaultUnlockEncryptsLeftovers(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetChat(fullChat("content")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChat(detailChat("details")); err != nil {
		t.Fatal(err)
	}
	setDetailSettings(t, s)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Put the details back in plain text, as an earlier version left them.
	raw, err := sql.Open("sqlite3", s.DBPath+"?_secure_delete=off")
	if err != nil {
		t.Fatal(err)
	}
	plain := detailChat("details")
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`UPDATE chats SET created_at = ? WHERE id = 'details'`, []any{vaultDetailTime}},
		{`UPDATE messages SET role = 'user', created_at = ?, updated_at = ? WHERE chat_id = 'details' AND id = (SELECT MIN(id) FROM messages WHERE chat_id = 'details')`, []any{vaultDetailTime, vaultDetailTime}},
		{`UPDATE messages SET role = 'assistant', model_name = ?, created_at = ?, updated_at = ?, thinking_time_start = ?, thinking_time_end = ? WHERE chat_id = 'details' AND id = (SELECT MAX(id) FROM messages WHERE chat_id = 'details')`, []any{
			plain.Messages[1].Model, vaultDetailTime, plain.Messages[1].UpdatedAt, *plain.Messages[1].ThinkingTimeStart, *plain.Messages[1].ThinkingTimeEnd,
		}},
		{`UPDATE tool_calls SET type = ?, function_name = ?`, []any{vaultDetailSecret + "-type", vaultDetailSecret + "-tool"}},
		{`UPDATE settings SET selected_model = ?, working_dir = ?`, []any{vaultDetailSecret + "-selected", "/" + vaultDetailSecret + "-folder"}},
		{`INSERT INTO users (name, email, plan) VALUES (?, ?, 'pro')`, []any{vaultDetailSecret + "-name", vaultDetailSecret + "@example.com"}},
		{`PRAGMA wal_checkpoint(TRUNCATE)`, nil},
	} {
		if _, err := raw.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatalf("%s: %v", stmt.query, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := &Store{DBPath: s.DBPath}
	defer reopened.Close()

	// Locked, the leftovers are still readable as before.
	settings, err := reopened.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SelectedModel != vaultDetailSecret+"-selected" {
		t.Fatalf("plain setting not readable while locked: %q", settings.SelectedModel)
	}

	if err := reopened.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	assertAllSealed(t, s.DBPath)
	assertValuesNotOnDisk(t, s.DBPath, vaultDetailSecret, vaultDetailDate)

	chat, err := reopened.Chat("details")
	if err != nil {
		t.Fatal(err)
	}
	assertDetailChat(t, chat)
	content, err := reopened.Chat("content")
	if err != nil {
		t.Fatal(err)
	}
	assertFullChat(t, content)
	assertDetailSettings(t, reopened)
}

func TestVaultChatListOrder(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	add := func(id string, created time.Time, updates ...time.Time) {
		t.Helper()
		chat := NewChat(id)
		chat.CreatedAt = created
		for i, u := range updates {
			role := "assistant"
			if i == 1 {
				role = "user"
			}
			msg := NewMessage(role, id+" "+role, nil)
			msg.CreatedAt, msg.UpdatedAt = created, u
			chat.Messages = append(chat.Messages, msg)
		}
		if err := s.SetChat(*chat); err != nil {
			t.Fatal(err)
		}
	}
	// "middle" was created first but updated after "oldest". "empty" has no
	// messages, so its creation time counts. The first message of each chat
	// is from the assistant, so the excerpt must skip it.
	add("oldest", base.Add(time.Hour), base.Add(2*time.Hour), base.Add(3*time.Hour))
	add("middle", base, base.Add(5*time.Hour), base.Add(4*time.Hour))
	add("empty", base.Add(6*time.Hour))
	add("newest", base.Add(time.Hour), base.Add(7*time.Hour), base.Add(8*time.Hour))

	chats, err := s.Chats()
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range chats {
		order = append(order, c.ID)
	}
	if fmt.Sprint(order) != "[newest empty middle oldest]" {
		t.Fatalf("unexpected order %v", order)
	}

	for _, c := range chats {
		if c.ID == "empty" {
			if len(c.Messages) != 0 {
				t.Errorf("empty chat has an excerpt: %+v", c.Messages)
			}
			continue
		}
		if len(c.Messages) != 1 || c.Messages[0].Role != "user" || c.Messages[0].Content != c.ID+" user" {
			t.Errorf("%s: unexpected excerpt %+v", c.ID, c.Messages)
		}
	}
	if !chats[2].Messages[0].UpdatedAt.Equal(base.Add(5 * time.Hour)) {
		t.Errorf("middle: excerpt time %v, want latest update", chats[2].Messages[0].UpdatedAt)
	}
}

// Settings are read and written before unlocking. That must neither reveal
// nor erase the encrypted ones.
func TestVaultLockedSettingsKeepEncryptedValues(t *testing.T) {
	s := privateTestStore(t)
	if err := s.SetupVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	setDetailSettings(t, s)
	s.LockVault()

	settings, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SelectedModel != "" || settings.WorkingDir != "" {
		t.Fatalf("encrypted settings visible while locked: %q %q", settings.SelectedModel, settings.WorkingDir)
	}
	settings.SidebarOpen = !settings.SidebarOpen
	if err := s.SetSettings(settings); err != nil {
		t.Fatal(err)
	}

	if err := s.UnlockVault(vaultPassword); err != nil {
		t.Fatal(err)
	}
	assertDetailSettings(t, s)
	unlocked, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.SidebarOpen != settings.SidebarOpen {
		t.Error("setting changed while locked was not saved")
	}
}
