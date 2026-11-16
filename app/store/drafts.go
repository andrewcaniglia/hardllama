package store

import (
	"crypto/hpke"
	"errors"
	"sync"
)

// Drafts keep what the user was typing when chats locked, so it's still
// there after unlocking.
//
// The window learns about a lock after the key is gone, so drafts are
// encrypted to a public key. Its private key is only kept encrypted with the
// vault key, so a draft can be saved while locked but only read after
// unlocking. Drafts are only held in memory, and are lost on quit.

const (
	draftKeyColumn = "draft_key"
	draftInfo      = "hardllama-draft-v1\x00"

	maxDraftSize = 1 << 20
	maxDrafts    = 32
)

var ErrDraftTooLarge = errors.New("draft is too large")

var (
	draftKEM  = hpke.MLKEM768X25519()
	draftKDF  = hpke.HKDFSHA256()
	draftAEAD = hpke.AES256GCM()
)

type drafts struct {
	mu        sync.Mutex
	publicKey hpke.PublicKey
	sealedKey []byte
	saved     map[string][]byte
}

// ensureKey makes sure drafts can be saved, keeping the current key if the
// vault key can still open it.
func (d *drafts) ensureKey(v *vaultCipher) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sealedKey != nil {
		if _, err := openValue(v, draftKeyColumn, d.sealedKey); err == nil {
			return nil
		}
	}
	key, err := draftKEM.GenerateKey()
	if err != nil {
		return err
	}
	return d.setKey(v, key)
}

// privateKey returns the private key's bytes, or nil if there isn't one.
func (d *drafts) privateKey(v *vaultCipher) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sealedKey == nil || v == nil {
		return nil, nil
	}
	return openValue(v, draftKeyColumn, d.sealedKey)
}

// reseal keeps the same key under a new vault key, so saved drafts survive a
// password change.
func (d *drafts) reseal(v *vaultCipher, private []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	key, err := draftKEM.NewPrivateKey(private)
	if err != nil {
		return err
	}
	return d.setKey(v, key)
}

// setKey must be called with d.mu held.
func (d *drafts) setKey(v *vaultCipher, key hpke.PrivateKey) error {
	private, err := key.Bytes()
	if err != nil {
		return err
	}
	sealed, err := sealValue(v, draftKeyColumn, private)
	if err != nil {
		return err
	}
	if d.sealedKey != nil && !samePublicKey(d.publicKey, key.PublicKey()) {
		// Drafts for the old key can't be read any more.
		d.saved = nil
	}
	d.publicKey, d.sealedKey = key.PublicKey(), sealed
	return nil
}

func samePublicKey(a, b hpke.PublicKey) bool {
	if a == nil || b == nil {
		return false
	}
	return string(a.Bytes()) == string(b.Bytes())
}

func (d *drafts) save(chatID, content string) error {
	if len(content) > maxDraftSize {
		return ErrDraftTooLarge
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if content == "" {
		delete(d.saved, chatID)
		return nil
	}
	if d.publicKey == nil {
		// Nothing has been unlocked since HardLlama started, so nothing can
		// have been typed.
		return ErrVaultLocked
	}
	if _, ok := d.saved[chatID]; !ok && len(d.saved) >= maxDrafts {
		return ErrDraftTooLarge
	}
	sealed, err := hpke.Seal(d.publicKey, draftKDF, draftAEAD, []byte(draftInfo+chatID), []byte(content))
	if err != nil {
		return err
	}
	if d.saved == nil {
		d.saved = map[string][]byte{}
	}
	d.saved[chatID] = sealed
	return nil
}

func (d *drafts) take(v *vaultCipher, chatID string) (string, error) {
	if v == nil {
		return "", ErrVaultLocked
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	sealed, ok := d.saved[chatID]
	if !ok {
		return "", nil
	}
	private, err := openValue(v, draftKeyColumn, d.sealedKey)
	if err != nil {
		return "", err
	}
	key, err := draftKEM.NewPrivateKey(private)
	if err != nil {
		return "", err
	}
	content, err := hpke.Open(key, draftKDF, draftAEAD, []byte(draftInfo+chatID), sealed)
	if err != nil {
		return "", ErrVaultDecryptFail
	}
	delete(d.saved, chatID)
	return string(content), nil
}

// SaveDraft keeps what the user was typing in a chat, replacing any earlier
// draft for it. It works while chats are locked. An empty draft removes it.
func (s *Store) SaveDraft(chatID, content string) error {
	if err := s.ensureDB(); err != nil {
		return err
	}
	if !s.db.vaultEnabled.Load() {
		return nil
	}
	return s.drafts.save(chatID, content)
}

// TakeDraft returns and forgets the draft for a chat, or "" if there isn't
// one. It fails while chats are locked.
func (s *Store) TakeDraft(chatID string) (string, error) {
	if err := s.ensureDB(); err != nil {
		return "", err
	}
	if !s.db.vaultEnabled.Load() {
		return "", nil
	}
	return s.drafts.take(s.db.vault.Load(), chatID)
}
