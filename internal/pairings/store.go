package pairings

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusPaired  Status = "paired"
	StatusExpired Status = "expired"
)

var (
	ErrNotFound      = errors.New("pairing not found")
	ErrAlreadyPaired = errors.New("pairing already paired")
	ErrExpired       = errors.New("pairing expired")
)

type Pairing struct {
	ID             string    `json:"id"`
	Token          string    `json:"token"`
	InstallationID string    `json:"installationId"`
	ChannelID      string    `json:"channelId,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	ClaimedAt      time.Time `json:"claimedAt,omitempty"`
}

func (p Pairing) StatusAt(now time.Time) Status {
	if !p.ClaimedAt.IsZero() {
		return StatusPaired
	}
	if !p.ExpiresAt.After(now) {
		return StatusExpired
	}
	return StatusPending
}

type Store struct {
	mu        sync.RWMutex
	byID      map[string]Pairing
	byToken   map[string]string
	statePath string
}

type persistedState struct {
	Pairings []Pairing `json:"pairings"`
}

func NewStore() *Store {
	return newStore("")
}

func OpenStore(statePath string) (*Store, error) {
	store := newStore(statePath)
	if statePath == "" {
		return store, nil
	}

	state, err := readState(statePath)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return store, nil
	}

	now := time.Now().UTC()
	store.mu.Lock()
	defer store.mu.Unlock()

	for _, pairing := range state.Pairings {
		pairing.ID = strings.TrimSpace(pairing.ID)
		pairing.Token = strings.TrimSpace(pairing.Token)
		pairing.InstallationID = strings.TrimSpace(pairing.InstallationID)
		pairing.ChannelID = strings.TrimSpace(pairing.ChannelID)

		if pairing.ID == "" || pairing.Token == "" || pairing.InstallationID == "" {
			continue
		}

		if pairing.CreatedAt.IsZero() {
			pairing.CreatedAt = now
		}
		if pairing.ExpiresAt.IsZero() {
			pairing.ExpiresAt = pairing.CreatedAt.Add(10 * time.Minute)
		}

		store.byID[pairing.ID] = pairing
		store.byToken[pairing.Token] = pairing.ID
	}

	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byID:      make(map[string]Pairing),
		byToken:   make(map[string]string),
		statePath: statePath,
	}
}

func (s *Store) Create(installationID string, ttl time.Duration) (Pairing, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return Pairing{}, fmt.Errorf("installation ID is required")
	}

	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.cloneLocked()

	now := time.Now().UTC()
	pairing := Pairing{
		ID:             randomToken(12),
		Token:          randomToken(16),
		InstallationID: installationID,
		CreatedAt:      now,
		ExpiresAt:      now.Add(ttl),
	}

	s.byID[pairing.ID] = pairing
	s.byToken[pairing.Token] = pairing.ID

	if err := s.persistLocked(); err != nil {
		s.restoreLocked(previous)
		return Pairing{}, err
	}

	return pairing, nil
}

func (s *Store) GetByID(id string) (Pairing, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairing, ok := s.byID[strings.TrimSpace(id)]
	return pairing, ok
}

func (s *Store) GetByToken(token string) (Pairing, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pairingID, ok := s.byToken[strings.TrimSpace(token)]
	if !ok {
		return Pairing{}, false
	}

	pairing, found := s.byID[pairingID]
	return pairing, found
}

func (s *Store) ClaimByToken(token string, channelID string) (Pairing, error) {
	token = strings.TrimSpace(token)
	channelID = strings.TrimSpace(channelID)
	if token == "" {
		return Pairing{}, ErrNotFound
	}
	if channelID == "" {
		return Pairing{}, fmt.Errorf("channel ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pairingID, ok := s.byToken[token]
	if !ok {
		return Pairing{}, ErrNotFound
	}

	pairing, found := s.byID[pairingID]
	if !found {
		return Pairing{}, ErrNotFound
	}

	now := time.Now().UTC()
	switch pairing.StatusAt(now) {
	case StatusPaired:
		return Pairing{}, ErrAlreadyPaired
	case StatusExpired:
		return Pairing{}, ErrExpired
	default:
	}

	previous := pairing
	pairing.ChannelID = channelID
	pairing.ClaimedAt = now
	s.byID[pairingID] = pairing

	if err := s.persistLocked(); err != nil {
		s.byID[pairingID] = previous
		return Pairing{}, err
	}

	return pairing, nil
}

func (s *Store) cloneLocked() *Store {
	clone := newStore(s.statePath)
	for key, value := range s.byID {
		clone.byID[key] = value
	}
	for key, value := range s.byToken {
		clone.byToken[key] = value
	}
	return clone
}

func (s *Store) restoreLocked(snapshot *Store) {
	s.byID = snapshot.byID
	s.byToken = snapshot.byToken
}

func snapshotLocked(byID map[string]Pairing) []Pairing {
	out := make([]Pairing, 0, len(byID))
	for _, pairing := range byID {
		out = append(out, pairing)
	}

	slices.SortFunc(out, func(a, b Pairing) int {
		switch {
		case a.CreatedAt.Before(b.CreatedAt):
			return 1
		case a.CreatedAt.After(b.CreatedAt):
			return -1
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	return out
}

func readState(statePath string) (*persistedState, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	return &state, nil
}

func (s *Store) persistLocked() error {
	if s.statePath == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	payload, err := json.Marshal(persistedState{
		Pairings: snapshotLocked(s.byID),
	})
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tempPath := s.statePath + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}

	if err := os.Rename(tempPath, s.statePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace state: %w", err)
	}

	return nil
}

func randomToken(size int) string {
	if size <= 0 {
		size = 32
	}

	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UTC().UnixNano())
	}

	// URL-safe without padding to keep pairing links short and copy-friendly.
	return base64.RawURLEncoding.EncodeToString(buffer)
}
