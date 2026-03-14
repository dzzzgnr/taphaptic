package watchpairings

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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

const (
	defaultTTL        = 10 * time.Minute
	maxClaimAttempts  = 6
	defaultCodeLength = 4
)

var (
	ErrNotFound       = errors.New("watch pairing code not found")
	ErrExpired        = errors.New("watch pairing code expired")
	ErrAlreadyClaimed = errors.New("watch pairing code already claimed")
	ErrTooManyAttempt = errors.New("watch pairing code attempts exceeded")
	ErrInvalidCode    = errors.New("watch pairing code is invalid")
)

type PairingCode struct {
	ID             string    `json:"codeId"`
	InstallationID string    `json:"installationId"`
	ChannelID      string    `json:"channelId"`
	CodeHash       string    `json:"codeHash"`
	CreatedAt      time.Time `json:"createdAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	ClaimedAt      time.Time `json:"claimedAt,omitempty"`
	Attempts       int       `json:"attempts"`
}

type Store struct {
	mu        sync.RWMutex
	byID      map[string]PairingCode
	statePath string
}

type persistedState struct {
	Codes []PairingCode `json:"codes"`
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

	for _, code := range state.Codes {
		code.ID = strings.TrimSpace(code.ID)
		code.InstallationID = strings.TrimSpace(code.InstallationID)
		code.ChannelID = strings.TrimSpace(code.ChannelID)
		code.CodeHash = strings.TrimSpace(code.CodeHash)
		if code.ID == "" || code.InstallationID == "" || code.ChannelID == "" || code.CodeHash == "" {
			continue
		}
		if code.CreatedAt.IsZero() {
			code.CreatedAt = now
		}
		if code.ExpiresAt.IsZero() {
			code.ExpiresAt = code.CreatedAt.Add(defaultTTL)
		}
		store.byID[code.ID] = code
	}

	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byID:      make(map[string]PairingCode),
		statePath: statePath,
	}
}

func (s *Store) Create(installationID string, channelID string, ttl time.Duration) (PairingCode, string, error) {
	installationID = strings.TrimSpace(installationID)
	channelID = strings.TrimSpace(channelID)
	if installationID == "" {
		return PairingCode{}, "", fmt.Errorf("installation ID is required")
	}
	if channelID == "" {
		return PairingCode{}, "", fmt.Errorf("channel ID is required")
	}
	if ttl <= 0 {
		ttl = defaultTTL
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.cloneLocked()
	now := time.Now().UTC()
	s.pruneExpiredLocked(now)

	// Keep at most one active code per installation.
	for codeID, existing := range s.byID {
		if existing.InstallationID != installationID {
			continue
		}
		if !existing.ClaimedAt.IsZero() {
			continue
		}
		delete(s.byID, codeID)
	}

	var pair PairingCode
	var clearCode string
	created := false
	for tries := 0; tries < 10; tries++ {
		clearCode = randomNumericCode()
		if clearCode == "" {
			continue
		}

		hash := hashCode(clearCode)
		if hash == "" {
			continue
		}
		if s.hasActiveHashLocked(hash, now) {
			continue
		}

		pair = PairingCode{
			ID:             randomToken(12),
			InstallationID: installationID,
			ChannelID:      channelID,
			CodeHash:       hash,
			CreatedAt:      now,
			ExpiresAt:      now.Add(ttl),
		}
		s.byID[pair.ID] = pair
		created = true
		break
	}

	if !created {
		s.restoreLocked(previous)
		return PairingCode{}, "", errors.New("failed to generate watch pairing code")
	}

	if err := s.persistLocked(); err != nil {
		s.restoreLocked(previous)
		return PairingCode{}, "", err
	}

	return pair, clearCode, nil
}

func (s *Store) Claim(code string) (PairingCode, error) {
	normalized, err := normalizeCode(code)
	if err != nil {
		return PairingCode{}, err
	}
	codeHash := hashCode(normalized)
	if codeHash == "" {
		return PairingCode{}, ErrInvalidCode
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	candidateID := ""
	var candidate PairingCode
	for _, current := range s.byID {
		if current.CodeHash != codeHash {
			continue
		}
		if candidateID == "" || current.CreatedAt.After(candidate.CreatedAt) {
			candidateID = current.ID
			candidate = current
		}
	}

	if candidateID == "" {
		return PairingCode{}, ErrNotFound
	}

	if candidate.Attempts >= maxClaimAttempts {
		return PairingCode{}, ErrTooManyAttempt
	}

	snapshot := s.cloneLocked()
	candidate.Attempts++

	switch {
	case !candidate.ClaimedAt.IsZero():
		s.byID[candidateID] = candidate
		if persistErr := s.persistLocked(); persistErr != nil {
			s.restoreLocked(snapshot)
			return PairingCode{}, persistErr
		}
		return PairingCode{}, ErrAlreadyClaimed
	case !candidate.ExpiresAt.After(now):
		delete(s.byID, candidateID)
		if persistErr := s.persistLocked(); persistErr != nil {
			s.restoreLocked(snapshot)
			return PairingCode{}, persistErr
		}
		return PairingCode{}, ErrExpired
	}

	candidate.ClaimedAt = now
	s.byID[candidateID] = candidate

	if persistErr := s.persistLocked(); persistErr != nil {
		s.restoreLocked(snapshot)
		return PairingCode{}, persistErr
	}

	return candidate, nil
}

func (s *Store) GetByID(codeID string) (PairingCode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	code, ok := s.byID[strings.TrimSpace(codeID)]
	return code, ok
}

func (s *Store) hasActiveHashLocked(hash string, now time.Time) bool {
	for _, pair := range s.byID {
		if pair.CodeHash != hash {
			continue
		}
		if !pair.ClaimedAt.IsZero() {
			continue
		}
		if !pair.ExpiresAt.After(now) {
			continue
		}
		return true
	}
	return false
}

func (s *Store) pruneExpiredLocked(now time.Time) bool {
	pruned := false
	for codeID, pair := range s.byID {
		if pair.ExpiresAt.After(now) {
			continue
		}
		delete(s.byID, codeID)
		pruned = true
	}
	return pruned
}

func (s *Store) cloneLocked() map[string]PairingCode {
	clone := make(map[string]PairingCode, len(s.byID))
	for key, value := range s.byID {
		clone[key] = value
	}
	return clone
}

func (s *Store) restoreLocked(snapshot map[string]PairingCode) {
	s.byID = snapshot
}

func snapshotLocked(byID map[string]PairingCode) []PairingCode {
	out := make([]PairingCode, 0, len(byID))
	for _, pair := range byID {
		out = append(out, pair)
	}

	slices.SortFunc(out, func(a, b PairingCode) int {
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

	payload, err := json.Marshal(persistedState{Codes: snapshotLocked(s.byID)})
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

func hashCode(code string) string {
	normalized, err := normalizeCode(code)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func normalizeCode(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidCode
	}

	var digits strings.Builder
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '-':
			continue
		default:
			return "", ErrInvalidCode
		}
	}

	value := digits.String()
	if len(value) != defaultCodeLength {
		return "", ErrInvalidCode
	}
	return value, nil
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

func randomNumericCode() string {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		modulo := time.Now().UTC().UnixNano() % 10000
		if modulo < 0 {
			modulo = -modulo
		}
		return fmt.Sprintf("%04d", modulo)
	}

	value := binary.BigEndian.Uint32(raw[:]) % 10000
	return fmt.Sprintf("%04d", value)
}
