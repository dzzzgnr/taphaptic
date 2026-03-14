package installations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Installation struct {
	ID         string    `json:"id"`
	Token      string    `json:"token"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

type Store struct {
	mu        sync.RWMutex
	byID      map[string]Installation
	byToken   map[string]string
	statePath string
}

type persistedState struct {
	Installations []Installation `json:"installations"`
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

	for _, installation := range state.Installations {
		installation.ID = strings.TrimSpace(installation.ID)
		installation.Token = strings.TrimSpace(installation.Token)
		if installation.ID == "" || installation.Token == "" {
			continue
		}

		if installation.CreatedAt.IsZero() {
			installation.CreatedAt = now
		}
		if installation.LastUsedAt.IsZero() {
			installation.LastUsedAt = installation.CreatedAt
		}

		store.byID[installation.ID] = installation
		store.byToken[installation.Token] = installation.ID
	}

	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byID:      make(map[string]Installation),
		byToken:   make(map[string]string),
		statePath: statePath,
	}
}

func (s *Store) Create() (Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.cloneLocked()

	now := time.Now().UTC()
	installation := Installation{
		ID:         randomToken(12),
		Token:      randomToken(32),
		CreatedAt:  now,
		LastUsedAt: now,
	}

	s.byID[installation.ID] = installation
	s.byToken[installation.Token] = installation.ID

	if err := s.persistLocked(); err != nil {
		s.restoreLocked(previous)
		return Installation{}, err
	}

	return installation, nil
}

func (s *Store) GetByToken(token string) (Installation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	installationID, ok := s.byToken[strings.TrimSpace(token)]
	if !ok {
		return Installation{}, false
	}

	installation, found := s.byID[installationID]
	return installation, found
}

func (s *Store) TouchToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	installationID, ok := s.byToken[token]
	if !ok {
		return false
	}

	installation, found := s.byID[installationID]
	if !found {
		return false
	}

	previous := installation
	installation.LastUsedAt = time.Now().UTC()
	s.byID[installationID] = installation

	if err := s.persistLocked(); err != nil {
		s.byID[installationID] = previous
		return false
	}

	return true
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

func snapshotLocked(byID map[string]Installation) []Installation {
	out := make([]Installation, 0, len(byID))
	for _, installation := range byID {
		out = append(out, installation)
	}

	slices.SortFunc(out, func(a, b Installation) int {
		switch {
		case a.LastUsedAt.Before(b.LastUsedAt):
			return 1
		case a.LastUsedAt.After(b.LastUsedAt):
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
		Installations: snapshotLocked(s.byID),
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

	return hex.EncodeToString(buffer)
}
