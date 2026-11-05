package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/argon2"
)

// The vault encrypts chat content with a random data key. The data key is
// stored wrapped by a key derived from the user's password, and only exists
// unwrapped in memory while the vault is unlocked. Settings stay readable
// because they are needed before a password can be entered, and because the
// CLI reads this database too.

var (
	ErrVaultLocked      = errors.New("chats are locked")
	ErrVaultPassword    = errors.New("incorrect password")
	ErrVaultWeakPass    = fmt.Errorf("password must be at least %d characters", minVaultPasswordLength)
	ErrVaultEnabled     = errors.New("chat encryption is already enabled")
	ErrVaultDecryptFail = errors.New("failed to decrypt chat data")
)

const (
	minVaultPasswordLength = 8
	vaultIdleTimeout       = 15 * time.Minute

	vaultKeySize     = 32
	vaultSaltSize    = 16
	vaultArgonTime   = 3
	vaultArgonMemory = 64 * 1024 // KiB
	vaultArgonLanes  = 4

	vaultKeyContext = "ollama-vault-key-1"
)

// sealedPrefix marks encrypted values. Once the vault is enabled every sealed
// column has been encrypted, so the prefix cannot collide with user content.
var sealedPrefix = []byte("ollama-vault-1:")

// sealedColumns lists every column holding chat content. Writes to these
// columns must go through seal or sealBytes.
var sealedColumns = []struct {
	table, column string
	blob          bool
}{
	{table: "chats", column: "title"},
	{table: "chats", column: "browser_state"},
	{table: "messages", column: "content"},
	{table: "messages", column: "thinking"},
	{table: "messages", column: "tool_result"},
	{table: "tool_calls", column: "function_arguments"},
	{table: "tool_calls", column: "function_result"},
	{table: "attachments", column: "filename"},
	{table: "attachments", column: "data", blob: true},
}

type vaultCipher struct {
	aead cipher.AEAD
}

func newVaultCipher(key []byte) (*vaultCipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &vaultCipher{aead: aead}, nil
}

func (v *vaultCipher) encrypt(context string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plaintext, []byte(context)), nil
}

func (v *vaultCipher) decrypt(context string, ciphertext []byte) ([]byte, error) {
	size := v.aead.NonceSize()
	if len(ciphertext) < size {
		return nil, ErrVaultDecryptFail
	}
	plaintext, err := v.aead.Open(nil, ciphertext[:size], ciphertext[size:], []byte(context))
	if err != nil {
		return nil, ErrVaultDecryptFail
	}
	return plaintext, nil
}

func deriveVaultKey(password string, salt []byte, time, memory uint32, lanes uint8) []byte {
	return argon2.IDKey([]byte(password), salt, time, memory, lanes, vaultKeySize)
}

// sealBytes encrypts a value for column. It fails rather than returning
// plaintext if the vault is enabled but locked.
func (db *database) sealBytes(column string, plaintext []byte) ([]byte, error) {
	if !db.vaultEnabled.Load() {
		return plaintext, nil
	}
	v := db.vault.Load()
	if v == nil {
		return nil, ErrVaultLocked
	}
	if len(plaintext) == 0 {
		return plaintext, nil
	}
	return sealValue(v, column, plaintext)
}

func sealValue(v *vaultCipher, column string, plaintext []byte) ([]byte, error) {
	ciphertext, err := v.encrypt(column, plaintext)
	if err != nil {
		return nil, err
	}
	sealed := make([]byte, len(sealedPrefix)+base64.StdEncoding.EncodedLen(len(ciphertext)))
	copy(sealed, sealedPrefix)
	base64.StdEncoding.Encode(sealed[len(sealedPrefix):], ciphertext)
	return sealed, nil
}

func (db *database) seal(column, plaintext string) (string, error) {
	sealed, err := db.sealBytes(column, []byte(plaintext))
	return string(sealed), err
}

func (db *database) sealNull(column string, value sql.NullString) (sql.NullString, error) {
	if !value.Valid {
		return value, nil
	}
	sealed, err := db.seal(column, value.String)
	return sql.NullString{String: sealed, Valid: true}, err
}

func (db *database) openBytes(column string, data []byte) ([]byte, error) {
	if !db.vaultEnabled.Load() {
		return data, nil
	}
	v := db.vault.Load()
	if v == nil {
		return nil, ErrVaultLocked
	}
	if !bytes.HasPrefix(data, sealedPrefix) {
		return data, nil
	}

	encoded := data[len(sealedPrefix):]
	ciphertext := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Decode(ciphertext, encoded)
	if err != nil {
		return nil, ErrVaultDecryptFail
	}
	return v.decrypt(column, ciphertext[:n])
}

func (db *database) open(column, sealed string) (string, error) {
	plaintext, err := db.openBytes(column, []byte(sealed))
	return string(plaintext), err
}

func (db *database) loadVaultState() error {
	var count int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM vault`).Scan(&count); err != nil {
		return fmt.Errorf("read vault state: %w", err)
	}
	db.vaultEnabled.Store(count > 0)
	return nil
}

func (db *database) vaultLocked() bool {
	return db.vaultEnabled.Load() && db.vault.Load() == nil
}

// setupVault enables encryption and encrypts all existing chats. The old
// plaintext is then removed from the database file and its WAL.
func (db *database) setupVault(password string) error {
	if len(password) < minVaultPasswordLength {
		return ErrVaultWeakPass
	}
	if db.vaultEnabled.Load() {
		return ErrVaultEnabled
	}

	key := make([]byte, vaultKeySize)
	salt := make([]byte, vaultSaltSize)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	v, err := newVaultCipher(key)
	if err != nil {
		return err
	}
	wrapping, err := newVaultCipher(deriveVaultKey(password, salt, vaultArgonTime, vaultArgonMemory, vaultArgonLanes))
	if err != nil {
		return err
	}
	wrapped, err := wrapping.encrypt(vaultKeyContext, key)
	if err != nil {
		return err
	}
	clear(key)

	// Transactions take the write lock when they begin, so no other write can
	// land between encrypting the existing rows and enabling the vault.
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, col := range sealedColumns {
		if err := sealExistingColumn(tx, v, col.table, col.column, col.blob); err != nil {
			return fmt.Errorf("encrypt %s.%s: %w", col.table, col.column, err)
		}
	}

	if _, err := tx.Exec(
		`INSERT INTO vault (id, salt, argon_time, argon_memory, argon_lanes, wrapped_key) VALUES (1, ?, ?, ?, ?, ?)`,
		salt, vaultArgonTime, vaultArgonMemory, vaultArgonLanes, wrapped,
	); err != nil {
		return fmt.Errorf("save vault: %w", err)
	}

	db.vault.Store(v)
	db.vaultEnabled.Store(true)
	if err := tx.Commit(); err != nil {
		db.vaultEnabled.Store(false)
		db.vault.Store(nil)
		return fmt.Errorf("commit vault: %w", err)
	}

	if _, err := db.conn.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("remove plaintext: %w", err)
	}
	if _, err := db.conn.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("remove plaintext: %w", err)
	}
	return nil
}

func sealExistingColumn(tx *sql.Tx, v *vaultCipher, table, column string, blob bool) error {
	type row struct {
		id    int64
		value []byte
	}

	// Table and column names come from sealedColumns, not from input.
	rows, err := tx.Query(fmt.Sprintf(`SELECT rowid, %[1]s FROM %[2]s WHERE %[1]s IS NOT NULL AND length(%[1]s) > 0`, column, table))
	if err != nil {
		return err
	}
	var values []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.value); err != nil {
			rows.Close()
			return err
		}
		values = append(values, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	update := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, table, column)
	for _, r := range values {
		sealed, err := sealValue(v, column, r.value)
		if err != nil {
			return err
		}
		var value any = string(sealed)
		if blob {
			value = sealed
		}
		if _, err := tx.Exec(update, value, r.id); err != nil {
			return err
		}
	}
	return nil
}

func (db *database) unlockVault(password string) error {
	var salt, wrapped []byte
	var time, memory uint32
	var lanes uint8
	err := db.conn.QueryRow(
		`SELECT salt, argon_time, argon_memory, argon_lanes, wrapped_key FROM vault WHERE id = 1`,
	).Scan(&salt, &time, &memory, &lanes, &wrapped)
	if err != nil {
		return fmt.Errorf("read vault: %w", err)
	}

	wrapping, err := newVaultCipher(deriveVaultKey(password, salt, time, memory, lanes))
	if err != nil {
		return err
	}
	key, err := wrapping.decrypt(vaultKeyContext, wrapped)
	if err != nil {
		return ErrVaultPassword
	}
	defer clear(key)

	v, err := newVaultCipher(key)
	if err != nil {
		return err
	}
	db.vault.Store(v)
	return nil
}

// VaultStatus reports whether chat encryption is enabled and currently locked.
func (s *Store) VaultStatus() (enabled, locked bool, err error) {
	if err := s.ensureDB(); err != nil {
		return false, false, err
	}
	return s.db.vaultEnabled.Load(), s.db.vaultLocked(), nil
}

// SetupVault enables chat encryption. The password cannot be recovered, and
// without it the encrypted chats are lost.
func (s *Store) SetupVault(password string) error {
	if err := s.ensureDB(); err != nil {
		return err
	}
	if err := s.db.setupVault(password); err != nil {
		return err
	}
	s.startVaultTimer()
	return nil
}

// UnlockVault makes chats readable until LockVault is called.
func (s *Store) UnlockVault(password string) error {
	if err := s.ensureDB(); err != nil {
		return err
	}
	if !s.db.vaultEnabled.Load() {
		return nil
	}
	if err := s.db.unlockVault(password); err != nil {
		return err
	}
	s.startVaultTimer()
	return nil
}

// LockVault discards the key and any private chats held in memory. It does
// nothing if encryption is not enabled.
func (s *Store) LockVault() {
	if s.ensureDB() != nil || !s.db.vaultEnabled.Load() {
		return
	}
	s.db.vault.Store(nil)
	s.private.clear()

	s.vaultTimerMu.Lock()
	defer s.vaultTimerMu.Unlock()
	if s.vaultTimer != nil {
		s.vaultTimer.Stop()
		s.vaultTimer = nil
	}
}

func (s *Store) startVaultTimer() {
	s.vaultTimerMu.Lock()
	defer s.vaultTimerMu.Unlock()
	if s.vaultTimer != nil {
		s.vaultTimer.Stop()
	}
	s.vaultTimer = time.AfterFunc(vaultIdleTimeout, s.LockVault)
}

// touchVault restarts the idle timer.
func (s *Store) touchVault() {
	s.vaultTimerMu.Lock()
	defer s.vaultTimerMu.Unlock()
	if s.vaultTimer != nil {
		s.vaultTimer.Reset(vaultIdleTimeout)
	}
}

// chatDB opens the database for chat access, failing while chats are locked.
func (s *Store) chatDB() error {
	if err := s.ensureDB(); err != nil {
		return err
	}
	if s.db.vaultLocked() {
		return ErrVaultLocked
	}
	s.touchVault()
	return nil
}
