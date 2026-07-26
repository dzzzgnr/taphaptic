package devices

import (
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

var (
	ErrInvalidToken = errors.New("invalid APNs device token")
	ErrNotFound     = errors.New("watch device registration not found")
)

type Device struct {
	InstallationID      string    `json:"installationId"`
	WatchInstallationID string    `json:"watchInstallationId"`
	Token               string    `json:"token"`
	Environment         string    `json:"environment"`
	Topic               string    `json:"topic"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type Store struct {
	mu        sync.RWMutex
	byWatchID map[string]Device
	statePath string
}

type persistedState struct {
	Devices []Device `json:"devices"`
}

func NewStore() *Store {
	return newStore("")
}

func OpenStore(statePath string) (*Store, error) {
	store := newStore(statePath)
	if strings.TrimSpace(statePath) == "" {
		return store, nil
	}
	raw, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read device registrations: %w", err)
	}

	var state persistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode device registrations: %w", err)
	}
	for _, device := range state.Devices {
		normalized, err := normalizeDevice(device)
		if err != nil {
			continue
		}
		store.byWatchID[normalized.WatchInstallationID] = normalized
	}
	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byWatchID: make(map[string]Device),
		statePath: statePath,
	}
}

func (s *Store) Upsert(device Device) (Device, error) {
	normalized, err := normalizeDevice(device)
	if err != nil {
		return Device{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous := cloneDevices(s.byWatchID)
	now := time.Now().UTC()
	if existing, ok := s.byWatchID[normalized.WatchInstallationID]; ok {
		normalized.CreatedAt = existing.CreatedAt
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now
	s.byWatchID[normalized.WatchInstallationID] = normalized
	if err := s.persistLocked(); err != nil {
		s.byWatchID = previous
		return Device{}, err
	}
	return normalized, nil
}

func (s *Store) ForInstallation(installationID string) []Device {
	installationID = strings.TrimSpace(installationID)
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Device, 0)
	for _, device := range s.byWatchID {
		if device.InstallationID == installationID {
			out = append(out, device)
		}
	}
	slices.SortFunc(out, func(a, b Device) int {
		return strings.Compare(a.WatchInstallationID, b.WatchInstallationID)
	})
	return out
}

func (s *Store) Remove(installationID, watchInstallationID string) error {
	installationID = strings.TrimSpace(installationID)
	watchInstallationID = strings.TrimSpace(watchInstallationID)

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byWatchID[watchInstallationID]
	if !ok || existing.InstallationID != installationID {
		return ErrNotFound
	}
	previous := cloneDevices(s.byWatchID)
	delete(s.byWatchID, watchInstallationID)
	if err := s.persistLocked(); err != nil {
		s.byWatchID = previous
		return err
	}
	return nil
}

func (s *Store) RemoveByToken(token string) bool {
	token = normalizeToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := cloneDevices(s.byWatchID)
	removed := false
	for watchID, device := range s.byWatchID {
		if device.Token == token {
			delete(s.byWatchID, watchID)
			removed = true
		}
	}
	if !removed {
		return false
	}
	if err := s.persistLocked(); err != nil {
		s.byWatchID = previous
		return false
	}
	return true
}

func (s *Store) RemoveForInstallation(installationID string) error {
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := cloneDevices(s.byWatchID)
	removed := false
	for watchID, device := range s.byWatchID {
		if device.InstallationID == installationID {
			delete(s.byWatchID, watchID)
			removed = true
		}
	}
	if !removed {
		return nil
	}
	if err := s.persistLocked(); err != nil {
		s.byWatchID = previous
		return err
	}
	return nil
}

func normalizeDevice(device Device) (Device, error) {
	device.InstallationID = strings.TrimSpace(device.InstallationID)
	device.WatchInstallationID = strings.TrimSpace(device.WatchInstallationID)
	device.Token = normalizeToken(device.Token)
	device.Environment = strings.ToLower(strings.TrimSpace(device.Environment))
	device.Topic = strings.TrimSpace(device.Topic)

	if device.InstallationID == "" || device.WatchInstallationID == "" {
		return Device{}, errors.New("installationId and watchInstallationId are required")
	}
	decoded, err := hex.DecodeString(device.Token)
	if err != nil || len(decoded) != 32 {
		return Device{}, ErrInvalidToken
	}
	switch device.Environment {
	case "", "production":
		device.Environment = "production"
	case "development", "sandbox":
		device.Environment = "development"
	default:
		return Device{}, fmt.Errorf("invalid APNs environment %q", device.Environment)
	}
	if device.Topic == "" {
		return Device{}, errors.New("APNs topic is required")
	}
	return device, nil
}

func normalizeToken(token string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(token)), ""))
}

func cloneDevices(input map[string]Device) map[string]Device {
	out := make(map[string]Device, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (s *Store) persistLocked() error {
	if strings.TrimSpace(s.statePath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o700); err != nil {
		return fmt.Errorf("create device state directory: %w", err)
	}
	devices := make([]Device, 0, len(s.byWatchID))
	for _, device := range s.byWatchID {
		devices = append(devices, device)
	}
	slices.SortFunc(devices, func(a, b Device) int {
		return strings.Compare(a.WatchInstallationID, b.WatchInstallationID)
	})
	raw, err := json.Marshal(persistedState{Devices: devices})
	if err != nil {
		return fmt.Errorf("encode device registrations: %w", err)
	}
	tempPath := s.statePath + ".tmp"
	if err := os.WriteFile(tempPath, raw, 0o600); err != nil {
		return fmt.Errorf("write device registrations: %w", err)
	}
	if err := os.Rename(tempPath, s.statePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace device registrations: %w", err)
	}
	return nil
}
