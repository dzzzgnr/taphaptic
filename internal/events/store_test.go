package events

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseType(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   Type
		wantOK bool
	}{
		{name: "completed", input: "completed", want: TypeCompleted, wantOK: true},
		{name: "completed mixed case", input: " Completed ", want: TypeCompleted, wantOK: true},
		{name: "subagent completed", input: "subagent_completed", want: TypeSubagentCompleted, wantOK: true},
		{name: "failed", input: "failed", want: TypeFailed, wantOK: true},
		{name: "attention", input: "attention", want: TypeAttention, wantOK: true},
		{name: "invalid", input: "noop", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseType(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ParseType(%q) ok=%v want %v", tt.input, ok, tt.wantOK)
			}

			if got != tt.want {
				t.Fatalf("ParseType(%q)=%q want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewStoreSeedsIDsFromCurrentTime(t *testing.T) {
	before := time.Now().UTC().UnixMilli()
	store := NewStore(4)

	event, err := store.Append(TypeCompleted)
	if err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	after := time.Now().UTC().UnixMilli()

	if event.ID < before || event.ID > after+1 {
		t.Fatalf("expected first event ID to be seeded from current time, got %d (before=%d after=%d)", event.ID, before, after)
	}
}

func TestOpenStoreLoadsPersistedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "events.json")

	store, err := OpenStore(4, statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	first, err := store.Append(TypeCompleted)
	if err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	second, err := store.Append(TypeFailed)
	if err != nil {
		t.Fatalf("Append returned error: %v", err)
	}

	reloaded, err := OpenStore(4, statePath)
	if err != nil {
		t.Fatalf("OpenStore reload returned error: %v", err)
	}

	events := reloaded.Since(0)
	if len(events) != 2 {
		t.Fatalf("expected 2 events after reload, got %d", len(events))
	}

	if events[0].ID != first.ID || events[1].ID != second.ID {
		t.Fatalf("unexpected event IDs after reload: got [%d %d], want [%d %d]", events[0].ID, events[1].ID, first.ID, second.ID)
	}

	third, err := reloaded.Append(TypeCompleted)
	if err != nil {
		t.Fatalf("Append after reload returned error: %v", err)
	}

	if third.ID <= second.ID {
		t.Fatalf("expected Append after reload to continue incrementing IDs, got %d after %d", third.ID, second.ID)
	}
}

func TestAppendInputPersistsMetadata(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "events.json")

	store, err := OpenStore(4, statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	event, err := store.AppendInput(AppendInput{
		Type:   TypeAttention,
		Source: "claude-code",
		Title:  "Claude Code needs attention",
		Body:   "Claude Code is waiting for permission",
	})
	if err != nil {
		t.Fatalf("AppendInput returned error: %v", err)
	}

	reloaded, err := OpenStore(4, statePath)
	if err != nil {
		t.Fatalf("OpenStore reload returned error: %v", err)
	}

	latest, ok := reloaded.Latest()
	if !ok {
		t.Fatalf("Latest returned no event")
	}

	if latest.ID != event.ID {
		t.Fatalf("Latest returned wrong event ID: got %d want %d", latest.ID, event.ID)
	}

	if latest.Source != "claude-code" || latest.Title != "Claude Code needs attention" || latest.Body != "Claude Code is waiting for permission" {
		t.Fatalf("Latest returned wrong metadata: %+v", latest)
	}
}

func TestSinceForChannelFiltersEvents(t *testing.T) {
	store := NewStore(8)

	if _, err := store.AppendInput(AppendInput{
		Type:      TypeCompleted,
		ChannelID: "channel-a",
	}); err != nil {
		t.Fatalf("AppendInput first event returned error: %v", err)
	}

	if _, err := store.AppendInput(AppendInput{
		Type:      TypeFailed,
		ChannelID: "channel-b",
	}); err != nil {
		t.Fatalf("AppendInput second event returned error: %v", err)
	}

	channelA := store.SinceForChannel("channel-a", 0)
	if len(channelA) != 1 {
		t.Fatalf("SinceForChannel returned %d events, want 1", len(channelA))
	}

	if channelA[0].ChannelID != "channel-a" {
		t.Fatalf("SinceForChannel returned wrong channel: got %q", channelA[0].ChannelID)
	}
}
