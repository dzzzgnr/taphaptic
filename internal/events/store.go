package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Type string

const (
	TypeCompleted         Type = "completed"
	TypeSubagentCompleted Type = "subagent_completed"
	TypeFailed            Type = "failed"
	TypeAttention         Type = "attention"
)

type Event struct {
	ID            int64     `json:"id"`
	SchemaVersion int       `json:"schemaVersion,omitempty"`
	ChannelID     string    `json:"channelId,omitempty"`
	Type          Type      `json:"type"`
	CreatedAt     time.Time `json:"createdAt"`
	OccurredAt    time.Time `json:"occurredAt,omitempty"`
	Source        string    `json:"source,omitempty"`
	SourceEvent   string    `json:"sourceEvent,omitempty"`
	DedupeKey     string    `json:"dedupeKey,omitempty"`
	Title         string    `json:"title,omitempty"`
	Body          string    `json:"body,omitempty"`
}

type AppendInput struct {
	SchemaVersion int
	ChannelID     string
	Type          Type
	OccurredAt    time.Time
	Source        string
	SourceEvent   string
	DedupeKey     string
	Title         string
	Body          string
}

type Store struct {
	mu        sync.RWMutex
	events    []Event
	maxEvents int
	nextID    int64
	statePath string
}

func NewStore(maxEvents int) *Store {
	return newStore(maxEvents, "")
}

func ParseType(raw string) (Type, bool) {
	switch Type(strings.ToLower(strings.TrimSpace(raw))) {
	case TypeCompleted:
		return TypeCompleted, true
	case TypeSubagentCompleted:
		return TypeSubagentCompleted, true
	case TypeFailed:
		return TypeFailed, true
	case TypeAttention:
		return TypeAttention, true
	default:
		return "", false
	}
}

func OpenStore(maxEvents int, statePath string) (*Store, error) {
	store := newStore(maxEvents, statePath)
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

	store.events = trimEvents(state.Events, store.maxEvents)

	maxEventID := int64(0)
	for _, event := range store.events {
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
	}

	store.nextID = state.NextID
	if store.nextID <= maxEventID {
		store.nextID = maxEventID + 1
	}
	if store.nextID <= 0 {
		store.nextID = seededNextID()
		if store.nextID <= maxEventID {
			store.nextID = maxEventID + 1
		}
	}

	return store, nil
}

func newStore(maxEvents int, statePath string) *Store {
	if maxEvents <= 0 {
		maxEvents = 32
	}

	return &Store{
		maxEvents: maxEvents,
		nextID:    seededNextID(),
		statePath: statePath,
	}
}

func (s *Store) Append(eventType Type) (Event, error) {
	return s.AppendInput(AppendInput{Type: eventType})
}

func (s *Store) AppendInput(input AppendInput) (Event, error) {
	event, _, err := s.AppendUnique(input)
	return event, err
}

// AppendUnique appends an event unless the same non-empty deduplication key
// already exists in the channel. The bool reports whether a new event was
// created.
func (s *Store) AppendUnique(input AppendInput) (Event, bool, error) {
	if _, ok := ParseType(string(input.Type)); !ok {
		return Event{}, false, fmt.Errorf("invalid event type %q", input.Type)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	channelID := strings.TrimSpace(input.ChannelID)
	dedupeKey := strings.TrimSpace(input.DedupeKey)
	if dedupeKey != "" {
		for index := len(s.events) - 1; index >= 0; index-- {
			existing := s.events[index]
			if strings.TrimSpace(existing.ChannelID) == channelID &&
				strings.TrimSpace(existing.DedupeKey) == dedupeKey {
				return existing, false, nil
			}
		}
	}

	previousEvents := append([]Event(nil), s.events...)
	previousNextID := s.nextID

	schemaVersion := input.SchemaVersion
	if schemaVersion <= 0 {
		schemaVersion = 1
	}
	occurredAt := input.OccurredAt.UTC()
	if input.OccurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	event := Event{
		ID:            s.nextID,
		SchemaVersion: schemaVersion,
		ChannelID:     channelID,
		Type:          input.Type,
		CreatedAt:     time.Now().UTC(),
		OccurredAt:    occurredAt,
		Source:        strings.TrimSpace(input.Source),
		SourceEvent:   strings.TrimSpace(input.SourceEvent),
		DedupeKey:     dedupeKey,
		Title:         strings.TrimSpace(input.Title),
		Body:          strings.TrimSpace(input.Body),
	}
	s.nextID++

	s.events = append(s.events, event)
	if len(s.events) > s.maxEvents {
		s.events = trimEvents(s.events, s.maxEvents)
	}

	if err := s.persistLocked(); err != nil {
		s.events = previousEvents
		s.nextID = previousNextID
		return Event{}, false, err
	}

	return event, true, nil
}

func (s *Store) Since(afterID int64) []Event {
	return s.SinceForChannel("", afterID)
}

func (s *Store) SinceForChannel(channelID string, afterID int64) []Event {
	channelID = strings.TrimSpace(channelID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.events) == 0 {
		return []Event{}
	}

	out := make([]Event, 0, len(s.events))
	for _, event := range s.events {
		if event.ID <= afterID {
			continue
		}
		if channelID != "" && strings.TrimSpace(event.ChannelID) != channelID {
			continue
		}
		if channelID == "" && strings.TrimSpace(event.ChannelID) != "" {
			continue
		}
		out = append(out, event)
	}

	return out
}

func (s *Store) Latest() (Event, bool) {
	return s.LatestForChannel("")
}

func (s *Store) LatestForChannel(channelID string) (Event, bool) {
	channelID = strings.TrimSpace(channelID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.events) == 0 {
		return Event{}, false
	}

	for index := len(s.events) - 1; index >= 0; index-- {
		event := s.events[index]
		if channelID != "" && strings.TrimSpace(event.ChannelID) != channelID {
			continue
		}
		if channelID == "" && strings.TrimSpace(event.ChannelID) != "" {
			continue
		}
		return event, true
	}

	return Event{}, false
}

func (s *Store) RemoveForChannel(channelID string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	previousEvents := append([]Event(nil), s.events...)
	filtered := make([]Event, 0, len(s.events))
	for _, event := range s.events {
		if strings.TrimSpace(event.ChannelID) != channelID {
			filtered = append(filtered, event)
		}
	}
	if len(filtered) == len(s.events) {
		return nil
	}
	s.events = filtered
	if err := s.persistLocked(); err != nil {
		s.events = previousEvents
		return err
	}
	return nil
}

type persistedState struct {
	NextID int64   `json:"nextID"`
	Events []Event `json:"events"`
}

func seededNextID() int64 {
	nextID := time.Now().UTC().UnixMilli()
	if nextID <= 0 {
		nextID = 1
	}
	return nextID
}

func trimEvents(events []Event, maxEvents int) []Event {
	if len(events) <= maxEvents {
		out := make([]Event, len(events))
		copy(out, events)
		return out
	}

	out := make([]Event, maxEvents)
	copy(out, events[len(events)-maxEvents:])
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
		NextID: s.nextID,
		Events: s.events,
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
