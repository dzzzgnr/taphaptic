package devices

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type Device struct {
	InstallationID string    `json:"installationId"`
	ChannelID      string    `json:"channelId,omitempty"`
	Platform       string    `json:"platform"`
	PushToken      string    `json:"pushToken"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type RegisterInput struct {
	InstallationID string
	ChannelID      string
	Platform       string
	PushToken      string
}

type Store struct {
	mu        sync.RWMutex
	byInstall map[string]Device
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

	for _, device := range state.Devices {
		if normalized, err := normalizeRegisterInput(RegisterInput{
			InstallationID: device.InstallationID,
			ChannelID:      device.ChannelID,
			Platform:       device.Platform,
			PushToken:      device.PushToken,
		}); err == nil {
			device.InstallationID = normalized.InstallationID
			device.ChannelID = normalized.ChannelID
			device.Platform = normalized.Platform
			device.PushToken = normalized.PushToken
			if device.CreatedAt.IsZero() {
				device.CreatedAt = time.Now().UTC()
			}
			if device.UpdatedAt.IsZero() {
				device.UpdatedAt = device.CreatedAt
			}
			store.byInstall[device.InstallationID] = device
		}
	}

	return store, nil
}

func newStore(statePath string) *Store {
	return &Store{
		byInstall: make(map[string]Device),
		statePath: statePath,
	}
}

func (s *Store) Upsert(input RegisterInput) (Device, error) {
	normalized, err := normalizeRegisterInput(input)
	if err != nil {
		return Device{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous := cloneMap(s.byInstall)

	now := time.Now().UTC()
	device, exists := s.byInstall[normalized.InstallationID]
	if !exists {
		device = Device{
			InstallationID: normalized.InstallationID,
			CreatedAt:      now,
		}
	}

	device.Platform = normalized.Platform
	device.ChannelID = normalized.ChannelID
	device.PushToken = normalized.PushToken
	device.UpdatedAt = now
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}

	for installationID, existing := range s.byInstall {
		if installationID == device.InstallationID {
			continue
		}
		if existing.PushToken == device.PushToken {
			delete(s.byInstall, installationID)
		}
	}

	s.byInstall[device.InstallationID] = device

	if err := s.persistLocked(); err != nil {
		s.byInstall = previous
		return Device{}, err
	}

	return device, nil
}

func (s *Store) All() []Device {
	return s.AllForChannel("")
}

func (s *Store) AllForChannel(channelID string) []Device {
	channelID = strings.TrimSpace(channelID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	all := snapshotLocked(s.byInstall)
	if channelID == "" {
		filtered := make([]Device, 0, len(all))
		for _, device := range all {
			if strings.TrimSpace(device.ChannelID) != "" {
				continue
			}
			filtered = append(filtered, device)
		}
		return filtered
	}

	filtered := make([]Device, 0, len(all))
	for _, device := range all {
		if strings.TrimSpace(device.ChannelID) != channelID {
			continue
		}
		filtered = append(filtered, device)
	}
	return filtered
}

func (s *Store) DeleteChannel(channelID string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous := cloneMap(s.byInstall)
	removed := false
	for installationID, device := range s.byInstall {
		if strings.TrimSpace(device.ChannelID) == channelID {
			delete(s.byInstall, installationID)
			removed = true
		}
	}

	if !removed {
		return nil
	}

	if err := s.persistLocked(); err != nil {
		s.byInstall = previous
		return err
	}

	return nil
}

type persistedState struct {
	Devices []Device `json:"devices"`
}

func normalizeRegisterInput(input RegisterInput) (RegisterInput, error) {
	input.InstallationID = strings.TrimSpace(input.InstallationID)
	if input.InstallationID == "" {
		return RegisterInput{}, fmt.Errorf("installation ID is required")
	}
	input.ChannelID = strings.TrimSpace(input.ChannelID)

	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	if input.Platform == "" {
		input.Platform = "ios"
	}
	if input.Platform != "ios" && input.Platform != "watchos" {
		return RegisterInput{}, fmt.Errorf("unsupported platform %q", input.Platform)
	}

	input.PushToken = normalizePushToken(input.PushToken)
	if input.PushToken == "" {
		return RegisterInput{}, fmt.Errorf("push token is required")
	}

	return input, nil
}

func normalizePushToken(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer(" ", "", "<", "", ">", "")
	raw = replacer.Replace(raw)
	if len(raw)%2 != 0 {
		return ""
	}

	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return ""
		}
	}

	return raw
}

func cloneMap(input map[string]Device) map[string]Device {
	out := make(map[string]Device, len(input))
	for key, value := range input {
		out[key] = value
	}
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
		Devices: snapshotLocked(s.byInstall),
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

func snapshotLocked(byInstall map[string]Device) []Device {
	out := make([]Device, 0, len(byInstall))
	for _, device := range byInstall {
		out = append(out, device)
	}

	slices.SortFunc(out, func(a, b Device) int {
		switch {
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		case a.UpdatedAt.After(b.UpdatedAt):
			return -1
		case a.InstallationID < b.InstallationID:
			return -1
		case a.InstallationID > b.InstallationID:
			return 1
		default:
			return 0
		}
	})

	return out
}
