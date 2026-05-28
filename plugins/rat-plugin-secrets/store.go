package main

// Encrypted secret storage. Each Secret holds an opaque AES-GCM ciphertext
// + nonce; the plaintext value is only ever in memory or in the GET
// response to authorised callers. The whole list is persisted to ratd
// as the plugin's config (the same mechanism rat-plugin-agents uses for
// its catalog) so restarts pick up where we left off.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Secret is one stored entry. The plaintext value never appears here —
// only the encrypted bytes + nonce that GCM needs to decrypt.
type Secret struct {
	Name        string    `json:"name"`
	CiphertextB64 string  `json:"ciphertext_b64"`
	NonceB64    string    `json:"nonce_b64"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SecretSummary is what list endpoints return — same fields minus the
// encrypted blob. Names + metadata, no key material.
type SecretSummary struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type store struct {
	key  []byte
	aead cipher.AEAD

	mu   sync.RWMutex
	byID map[string]*Secret
	cfg  *configStore
}

func newStore(key []byte, cfg *configStore) (*store, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return &store{
		key:  key,
		aead: aead,
		byID: map[string]*Secret{},
		cfg:  cfg,
	}, nil
}

// hydrate replaces the in-memory store from a persisted secret list —
// called on startup and on every config-poll tick.
func (s *store) hydrate(secrets []Secret) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string]*Secret, len(secrets))
	for i := range secrets {
		sec := secrets[i]
		s.byID[sec.Name] = &sec
	}
}

func (s *store) list() []SecretSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SecretSummary, 0, len(s.byID))
	for _, sec := range s.byID {
		out = append(out, SecretSummary{
			Name: sec.Name, Description: sec.Description,
			CreatedAt: sec.CreatedAt, UpdatedAt: sec.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// get returns the plaintext value for a secret, decrypting on the fly.
// Returns ErrNotFound if the name isn't in the store.
func (s *store) get(name string) (string, error) {
	s.mu.RLock()
	sec, ok := s.byID[name]
	s.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	ct, err := base64.StdEncoding.DecodeString(sec.CiphertextB64)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(sec.NonceB64)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	plain, err := s.aead.Open(nil, nonce, ct, []byte(name))
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// upsert creates or replaces a secret. Encrypts with a fresh random
// nonce each time (GCM is catastrophic if you reuse one). The name is
// used as AEAD additional data — protects against blob-swap attacks
// where an attacker might rename a stored secret to elevate access.
func (s *store) upsert(ctx context.Context, name, value, description string) (*Secret, error) {
	if name == "" {
		return nil, errors.New("name is required")
	}
	if value == "" {
		return nil, errors.New("value is required")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	ct := s.aead.Seal(nil, nonce, []byte(value), []byte(name))

	now := time.Now().UTC()
	s.mu.Lock()
	existing, ok := s.byID[name]
	createdAt := now
	if ok {
		createdAt = existing.CreatedAt
	}
	sec := &Secret{
		Name:          name,
		CiphertextB64: base64.StdEncoding.EncodeToString(ct),
		NonceB64:      base64.StdEncoding.EncodeToString(nonce),
		Description:   description,
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}
	s.byID[name] = sec
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.cfg.persist(ctx, snapshot); err != nil {
		// Roll back the in-memory change so the persisted state matches.
		s.mu.Lock()
		if ok {
			s.byID[name] = existing
		} else {
			delete(s.byID, name)
		}
		s.mu.Unlock()
		return nil, fmt.Errorf("persist: %w", err)
	}
	return sec, nil
}

func (s *store) delete(ctx context.Context, name string) error {
	s.mu.Lock()
	existing, ok := s.byID[name]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	delete(s.byID, name)
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.cfg.persist(ctx, snapshot); err != nil {
		// Roll back.
		s.mu.Lock()
		s.byID[name] = existing
		s.mu.Unlock()
		return fmt.Errorf("persist: %w", err)
	}
	return nil
}

func (s *store) snapshotLocked() []Secret {
	out := make([]Secret, 0, len(s.byID))
	for _, sec := range s.byID {
		out = append(out, *sec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ErrNotFound is returned when a secret name isn't in the store.
var ErrNotFound = errors.New("secret not found")
