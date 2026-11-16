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
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/argon2"
)

// The vault encrypts chats, and details about them, with a random data key.
// The data key is stored wrapped by a key derived from the user's password,
// and only exists unwrapped in memory while the vault is unlocked. Most
// settings stay readable because they are needed before a password can be
// entered, and because the CLI reads this database too. The ones that say
// something about the user, the selected model and working folder, are
// encrypted. The number of chats, messages and attachments, and their sizes,
// can still be seen.

var (
	ErrVaultLocked      = errors.New("chats are locked")
	ErrVaultPassword    = errors.New("incorrect password")
	ErrVaultWeakPass    = fmt.Errorf("password must be at least %d characters", minVaultPasswordLength)
	ErrVaultEnabled     = errors.New("chat encryption is already enabled")
	ErrVaultDecryptFail = errors.New("failed to decrypt chat data")
	ErrVaultNotEnabled  = errors.New("chat encryption is not enabled")
)

const (
	minVaultPasswordLength = 12
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

// sealedColumns lists every column holding chat content or details about
// chats, such as when they happened and which models were used. Writes to
// these columns must go through seal, sealBytes or sealTime.
var sealedColumns = []struct {
	table, column string
	blob          bool
}{
	{table: "chats", column: "title"},
	{table: "chats", column: "created_at"},
	{table: "chats", column: "browser_state"},
	{table: "messages", column: "role"},
	{table: "messages", column: "content"},
	{table: "messages", column: "thinking"},
	{table: "messages", column: "model_name"},
	{table: "messages", column: "created_at"},
	{table: "messages", column: "updated_at"},
	{table: "messages", column: "thinking_time_start"},
	{table: "messages", column: "thinking_time_end"},
	{table: "messages", column: "tool_result"},
	{table: "tool_calls", column: "type"},
	{table: "tool_calls", column: "function_name"},
	{table: "tool_calls", column: "function_arguments"},
	{table: "tool_calls", column: "function_result"},
	{table: "attachments", column: "filename"},
	{table: "attachments", column: "data", blob: true},
	{table: "settings", column: "selected_model"},
	{table: "settings", column: "working_dir"},
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
	return openValue(v, column, data)
}

// openValue decrypts a value that starts with sealedPrefix.
func openValue(v *vaultCipher, column string, data []byte) ([]byte, error) {
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

func (db *database) openNull(column string, value sql.NullString) (sql.NullString, error) {
	if !value.Valid {
		return value, nil
	}
	plaintext, err := db.open(column, value.String)
	return sql.NullString{String: plaintext, Valid: true}, err
}

// Timestamps are stored as text in the same format the SQLite driver uses, so
// that encrypted and unencrypted timestamps read back the same way. Reads
// must cast timestamp columns to text, because the driver turns text it
// can't parse as a time, such as an encrypted value, into the zero time.
var timestampFormat = sqlite3.SQLiteTimestampFormats[0]

func (db *database) sealTime(column string, t time.Time) (string, error) {
	return db.seal(column, t.Format(timestampFormat))
}

func (db *database) sealNullTime(column string, t *time.Time) (sql.NullString, error) {
	if t == nil {
		return sql.NullString{}, nil
	}
	sealed, err := db.sealTime(column, *t)
	return sql.NullString{String: sealed, Valid: true}, err
}

func (db *database) openTime(column, value string) (time.Time, error) {
	plaintext, err := db.open(column, value)
	if err != nil {
		return time.Time{}, err
	}
	return parseTimestamp(plaintext), nil
}

func (db *database) openNullTime(column string, value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	t, err := db.openTime(column, value.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// parseTimestamp parses a timestamp the same way the SQLite driver does,
// returning the zero time if it can't.
func parseTimestamp(s string) time.Time {
	s = strings.TrimSuffix(s, "Z")
	for _, format := range sqlite3.SQLiteTimestampFormats {
		if t, err := time.ParseInLocation(format, s, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
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

	v, salt, wrapped, err := newVaultKey(password)
	if err != nil {
		return err
	}

	// Transactions take the write lock when they begin, so no other write can
	// land between encrypting the existing rows and enabling the vault.
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := sealExistingColumns(tx, v); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM users`); err != nil {
		return fmt.Errorf("remove cached account: %w", err)
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

	return db.removeFreedPages()
}

// sealExistingColumns encrypts every value that is not encrypted yet. Values
// that are already encrypted are left alone, so this can run again after new
// columns are added to sealedColumns. It reports how many values it changed.
func sealExistingColumns(tx *sql.Tx, v *vaultCipher) (int, error) {
	total := 0
	for _, col := range sealedColumns {
		n, err := sealExistingColumn(tx, v, col.table, col.column, col.blob)
		if err != nil {
			return total, fmt.Errorf("encrypt %s.%s: %w", col.table, col.column, err)
		}
		total += n
	}
	return total, nil
}

func sealExistingColumn(tx *sql.Tx, v *vaultCipher, table, column string, blob bool) (int, error) {
	type row struct {
		id    int64
		value []byte
	}

	// Table and column names come from sealedColumns, not from input. Text
	// columns are cast so that timestamps are read as stored instead of being
	// parsed by the driver.
	value := column
	if !blob {
		value = fmt.Sprintf("CAST(%s AS TEXT)", column)
	}
	query := fmt.Sprintf(
		`SELECT rowid, %[3]s FROM %[2]s WHERE %[1]s IS NOT NULL AND length(%[1]s) > 0 AND CAST(substr(%[1]s, 1, %[4]d) AS TEXT) != ?`,
		column, table, value, len(sealedPrefix),
	)
	rows, err := tx.Query(query, string(sealedPrefix))
	if err != nil {
		return 0, err
	}
	var values []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.value); err != nil {
			rows.Close()
			return 0, err
		}
		if !bytes.HasPrefix(r.value, sealedPrefix) {
			values = append(values, r)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	update := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, table, column)
	for _, r := range values {
		sealed, err := sealValue(v, column, r.value)
		if err != nil {
			return 0, err
		}
		var value any = string(sealed)
		if blob {
			value = sealed
		}
		if _, err := tx.Exec(update, value, r.id); err != nil {
			return 0, err
		}
	}
	return len(values), nil
}

// removeFreedPages rewrites the database so that values which were replaced
// or deleted no longer exist anywhere in the file or its WAL.
func (db *database) removeFreedPages() error {
	if _, err := db.conn.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("remove plaintext: %w", err)
	}
	if _, err := db.conn.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("remove plaintext: %w", err)
	}
	return nil
}

// newVaultKey makes a random data key and wraps it with password.
func newVaultKey(password string) (v *vaultCipher, salt, wrapped []byte, err error) {
	key := make([]byte, vaultKeySize)
	defer clear(key)
	salt = make([]byte, vaultSaltSize)
	if _, err := rand.Read(key); err != nil {
		return nil, nil, nil, err
	}
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, nil, err
	}
	v, err = newVaultCipher(key)
	if err != nil {
		return nil, nil, nil, err
	}
	wrapping, err := newVaultCipher(deriveVaultKey(password, salt, vaultArgonTime, vaultArgonMemory, vaultArgonLanes))
	if err != nil {
		return nil, nil, nil, err
	}
	wrapped, err = wrapping.encrypt(vaultKeyContext, key)
	if err != nil {
		return nil, nil, nil, err
	}
	return v, salt, wrapped, nil
}

// unwrapVaultKey returns the data key if password is correct.
func (db *database) unwrapVaultKey(password string) (*vaultCipher, error) {
	var salt, wrapped []byte
	var time, memory uint32
	var lanes uint8
	err := db.conn.QueryRow(
		`SELECT salt, argon_time, argon_memory, argon_lanes, wrapped_key FROM vault WHERE id = 1`,
	).Scan(&salt, &time, &memory, &lanes, &wrapped)
	if err != nil {
		return nil, fmt.Errorf("read vault: %w", err)
	}

	wrapping, err := newVaultCipher(deriveVaultKey(password, salt, time, memory, lanes))
	if err != nil {
		return nil, err
	}
	key, err := wrapping.decrypt(vaultKeyContext, wrapped)
	if err != nil {
		return nil, ErrVaultPassword
	}
	defer clear(key)
	return newVaultCipher(key)
}

func (db *database) unlockVault(password string) error {
	v, err := db.unwrapVaultKey(password)
	if err != nil {
		return err
	}
	if err := db.sealLeftovers(v); err != nil {
		return err
	}
	db.vault.Store(v)
	return nil
}

// changeVaultPassword replaces the password and the data key, and
// re-encrypts everything with the new key. Replacing the key means an old
// copy of the database and the old password can't decrypt anything written
// afterwards. If anything fails, nothing changes.
func (db *database) changeVaultPassword(current, next string) error {
	if !db.vaultEnabled.Load() {
		return ErrVaultNotEnabled
	}
	if len(next) < minVaultPasswordLength {
		return ErrVaultWeakPass
	}
	old, err := db.unwrapVaultKey(current)
	if err != nil {
		return err
	}
	v, salt, wrapped, err := newVaultKey(next)
	if err != nil {
		return err
	}

	// Every write seals inside a transaction, and transactions take the write
	// lock when they begin. So while this one is open no other write can seal
	// with either key, and writes waiting for the lock seal with the new key.
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, col := range sealedColumns {
		if err := resealColumn(tx, old, v, col.table, col.column, col.blob); err != nil {
			return fmt.Errorf("re-encrypt %s.%s: %w", col.table, col.column, err)
		}
	}
	if _, err := tx.Exec(
		`UPDATE vault SET salt = ?, argon_time = ?, argon_memory = ?, argon_lanes = ?, wrapped_key = ? WHERE id = 1`,
		salt, vaultArgonTime, vaultArgonMemory, vaultArgonLanes, wrapped,
	); err != nil {
		return fmt.Errorf("save vault: %w", err)
	}

	previous := db.vault.Load()
	db.vault.Store(v)
	if err := tx.Commit(); err != nil {
		db.vault.Store(previous)
		return fmt.Errorf("commit vault: %w", err)
	}
	return db.removeFreedPages()
}

// resealColumn decrypts every value in a column with old and encrypts it
// with v. Values that aren't encrypted yet are encrypted with v.
func resealColumn(tx *sql.Tx, old, v *vaultCipher, table, column string, blob bool) error {
	type row struct {
		id    int64
		value []byte
	}

	// Table and column names come from sealedColumns, not from input.
	value := column
	if !blob {
		value = fmt.Sprintf("CAST(%s AS TEXT)", column)
	}
	rows, err := tx.Query(fmt.Sprintf(
		`SELECT rowid, %[3]s FROM %[2]s WHERE %[1]s IS NOT NULL AND length(%[1]s) > 0`,
		column, table, value,
	))
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
		plaintext := r.value
		if bytes.HasPrefix(r.value, sealedPrefix) {
			if plaintext, err = openValue(old, column, r.value); err != nil {
				return err
			}
		}
		sealed, err := sealValue(v, column, plaintext)
		clear(plaintext)
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

// sealLeftovers encrypts anything still stored in plain text, such as chat
// details from versions that only encrypted chat content, and removes the
// cached account. Unlocking fails if this fails, so plain text never stays
// behind silently.
func (db *database) sealLeftovers(v *vaultCipher) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	changed, err := sealExistingColumns(tx, v)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`DELETE FROM users`)
	if err != nil {
		return fmt.Errorf("remove cached account: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if changed == 0 && removed == 0 {
		return nil
	}
	return db.removeFreedPages()
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

// ChangeVaultPassword replaces the password. The vault stays unlocked.
func (s *Store) ChangeVaultPassword(current, next string) error {
	if err := s.ensureDB(); err != nil {
		return err
	}
	if err := s.db.changeVaultPassword(current, next); err != nil {
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
