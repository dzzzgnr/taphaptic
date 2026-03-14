package channels

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

type Channel struct {
	ID                 string    `json:"id"`
	InstallationID     string    `json:"installationId"`
	ClaudeSessionToken string    `json:"claudeSessionToken"`
	PhoneSessionToken  string    `json:"phoneSessionToken,omitempty"`
	WatchSessionToken  string    `json:"watchSessionToken,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	PairedAt           time.Time `json:"pairedAt,omitempty"`
}

type Store struct {
	mu             sync.RWMutex
	byID           map[string]Channel
	byInstallation map[string]string
	byClaudeToken  map[string]string
	byPhoneToken   map[string]string
	byWatchToken   map[string]string
	statePath      string
}

type persistedState struct {
	Channels []Channel `json:"channels"`
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

	now := time.Now().UTC()
	for _, channel := range state.Channels {
		channel.ID = strings.TrimSpace(channel.ID)
		channel.InstallationID = strings.TrimSpace(channel.InstallationID)
		channel.ClaudeSessionToken = strings.TrimSpace(channel.ClaudeSessionToken)
		channel.PhoneSessionToken = strings.TrimSpace(channel.PhoneSessionToken)
		channel.WatchSessionToken = strings.TrimSpace(channel.WatchSessionToken)

		if channel.ID == "" || channel.InstallationID == "" || channel.ClaudeSessionToken == "" {
			continue
		}

		if channel.CreatedAt.IsZero() {
			channel.CreatedAt = now
		}
		if channel.UpdatedAt.IsZero() {
			channel.UpdatedAt = channel.CreatedAt
		}

		store.byID[channel.ID] = channel
		store.byInstallation[channel.InstallationID] = channel.ID
		store.byClaudeToken[channel.ClaudeSessionToken] = channel.ID
		if channel.PhoneSessionToken != "" {
			store.byPhoneToken[channel.PhoneSessionToken] = channel.ID
		}
		if channel.WatchSessionToken != "" {
			store.byWatchToken[channel.WatchSessionToken] = channel.ID
		}
	}

	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byID:           make(map[string]Channel),
		byInstallation: make(map[string]string),
		byClaudeToken:  make(map[string]string),
		byPhoneToken:   make(map[string]string),
		byWatchToken:   make(map[string]string),
		statePath:      statePath,
	}
}

func (s *Store) EnsureForInstallation(installationID string) (Channel, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return Channel{}, fmt.Errorf("installation ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if channelID, ok := s.byInstallation[installationID]; ok {
		channel, found := s.byID[channelID]
		if found {
			return channel, nil
		}
	}

	previous := s.cloneLocked()
	now := time.Now().UTC()
	channel := Channel{
		ID:                 randomToken(16),
		InstallationID:     installationID,
		ClaudeSessionToken: randomToken(32),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	s.byID[channel.ID] = channel
	s.byInstallation[channel.InstallationID] = channel.ID
	s.byClaudeToken[channel.ClaudeSessionToken] = channel.ID

	if err := s.persistLocked(); err != nil {
		s.restoreLocked(previous)
		return Channel{}, err
	}

	return channel, nil
}

func (s *Store) RotatePhoneSession(installationID string) (Channel, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return Channel{}, fmt.Errorf("installation ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	channelID, ok := s.byInstallation[installationID]
	if !ok {
		return Channel{}, fmt.Errorf("installation %q is not registered", installationID)
	}

	channel, ok := s.byID[channelID]
	if !ok {
		return Channel{}, fmt.Errorf("channel %q is missing", channelID)
	}

	previous := s.cloneLocked()

	if channel.PhoneSessionToken != "" {
		delete(s.byPhoneToken, channel.PhoneSessionToken)
	}

	channel.PhoneSessionToken = randomToken(32)
	channel.UpdatedAt = time.Now().UTC()
	channel.PairedAt = channel.UpdatedAt

	s.byID[channel.ID] = channel
	s.byPhoneToken[channel.PhoneSessionToken] = channel.ID

	if err := s.persistLocked(); err != nil {
		s.restoreLocked(previous)
		return Channel{}, err
	}

	return channel, nil
}

func (s *Store) RotateWatchSession(installationID string) (Channel, error) {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return Channel{}, fmt.Errorf("installation ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	channelID, ok := s.byInstallation[installationID]
	if !ok {
		return Channel{}, fmt.Errorf("installation %q is not registered", installationID)
	}

	channel, ok := s.byID[channelID]
	if !ok {
		return Channel{}, fmt.Errorf("channel %q is missing", channelID)
	}

	previous := s.cloneLocked()

	if channel.WatchSessionToken != "" {
		delete(s.byWatchToken, channel.WatchSessionToken)
	}

	channel.WatchSessionToken = randomToken(32)
	channel.UpdatedAt = time.Now().UTC()
	channel.PairedAt = channel.UpdatedAt

	s.byID[channel.ID] = channel
	s.byWatchToken[channel.WatchSessionToken] = channel.ID

	if err := s.persistLocked(); err != nil {
		s.restoreLocked(previous)
		return Channel{}, err
	}

	return channel, nil
}

func (s *Store) GetByID(channelID string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channel, ok := s.byID[strings.TrimSpace(channelID)]
	return channel, ok
}

func (s *Store) GetByInstallation(installationID string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelID, ok := s.byInstallation[strings.TrimSpace(installationID)]
	if !ok {
		return Channel{}, false
	}

	channel, found := s.byID[channelID]
	return channel, found
}

func (s *Store) GetByClaudeToken(token string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelID, ok := s.byClaudeToken[strings.TrimSpace(token)]
	if !ok {
		return Channel{}, false
	}

	channel, found := s.byID[channelID]
	return channel, found
}

func (s *Store) GetByPhoneToken(token string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelID, ok := s.byPhoneToken[strings.TrimSpace(token)]
	if !ok {
		return Channel{}, false
	}

	channel, found := s.byID[channelID]
	return channel, found
}

func (s *Store) GetByWatchToken(token string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channelID, ok := s.byWatchToken[strings.TrimSpace(token)]
	if !ok {
		return Channel{}, false
	}

	channel, found := s.byID[channelID]
	return channel, found
}

func (s *Store) All() []Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return snapshotLocked(s.byID)
}

func (s *Store) cloneLocked() *Store {
	clone := newStore(s.statePath)

	for key, value := range s.byID {
		clone.byID[key] = value
	}
	for key, value := range s.byInstallation {
		clone.byInstallation[key] = value
	}
	for key, value := range s.byClaudeToken {
		clone.byClaudeToken[key] = value
	}
	for key, value := range s.byPhoneToken {
		clone.byPhoneToken[key] = value
	}
	for key, value := range s.byWatchToken {
		clone.byWatchToken[key] = value
	}

	return clone
}

func (s *Store) restoreLocked(snapshot *Store) {
	s.byID = snapshot.byID
	s.byInstallation = snapshot.byInstallation
	s.byClaudeToken = snapshot.byClaudeToken
	s.byPhoneToken = snapshot.byPhoneToken
	s.byWatchToken = snapshot.byWatchToken
}

func snapshotLocked(byID map[string]Channel) []Channel {
	out := make([]Channel, 0, len(byID))
	for _, channel := range byID {
		out = append(out, channel)
	}

	slices.SortFunc(out, func(a, b Channel) int {
		switch {
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		case a.UpdatedAt.After(b.UpdatedAt):
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
		Channels: snapshotLocked(s.byID),
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
