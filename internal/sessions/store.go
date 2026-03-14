package sessions

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

type Session struct {
	Token      string    `json:"token"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

type Store struct {
	mu        sync.RWMutex
	byToken   map[string]Session
	statePath string
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

	store.mu.Lock()
	defer store.mu.Unlock()

	for _, session := range state.Sessions {
		if session.Token == "" {
			continue
		}
		if session.CreatedAt.IsZero() {
			session.CreatedAt = time.Now().UTC()
		}
		if session.LastUsedAt.IsZero() {
			session.LastUsedAt = session.CreatedAt
		}
		store.byToken[session.Token] = session
	}

	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byToken:   make(map[string]Session),
		statePath: statePath,
	}
}

func (s *Store) Create() (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := cloneMap(s.byToken)

	now := time.Now().UTC()
	session := Session{
		Token:      randomToken(),
		CreatedAt:  now,
		LastUsedAt: now,
	}
	s.byToken[session.Token] = session

	if err := s.persistLocked(); err != nil {
		s.byToken = previous
		return Session{}, err
	}

	return session, nil
}

func (s *Store) Touch(token string) bool {
	if token == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.byToken[token]
	if !ok {
		return false
	}

	previous := session
	session.LastUsedAt = time.Now().UTC()
	s.byToken[token] = session

	if err := s.persistLocked(); err != nil {
		s.byToken[token] = previous
		return false
	}

	return true
}

type persistedState struct {
	Sessions []Session `json:"sessions"`
}

func randomToken() string {
	var buffer [32]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UTC().UnixNano())
	}

	return hex.EncodeToString(buffer[:])
}

func cloneMap(input map[string]Session) map[string]Session {
	out := make(map[string]Session, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func snapshotLocked(byToken map[string]Session) []Session {
	out := make([]Session, 0, len(byToken))
	for _, session := range byToken {
		out = append(out, session)
	}

	slices.SortFunc(out, func(a, b Session) int {
		switch {
		case a.LastUsedAt.Before(b.LastUsedAt):
			return 1
		case a.LastUsedAt.After(b.LastUsedAt):
			return -1
		case a.Token < b.Token:
			return -1
		case a.Token > b.Token:
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
		Sessions: snapshotLocked(s.byToken),
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
