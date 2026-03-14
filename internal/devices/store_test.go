package devices

import (
	"path/filepath"
	"testing"
)

func TestUpsertNormalizesTokenAndRemovesDuplicates(t *testing.T) {
	store := NewStore()

	first, err := store.Upsert(RegisterInput{
		InstallationID: "phone-a",
		PushToken:      "<AA BB CC DD>",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	if first.PushToken != "aabbccdd" {
		t.Fatalf("stored wrong token: got %q", first.PushToken)
	}

	if _, err := store.Upsert(RegisterInput{
		InstallationID: "phone-b",
		PushToken:      "aabbccdd",
	}); err != nil {
		t.Fatalf("second Upsert returned error: %v", err)
	}

	all := store.All()
	if len(all) != 1 {
		t.Fatalf("All returned %d devices, want 1", len(all))
	}

	if all[0].InstallationID != "phone-b" {
		t.Fatalf("expected latest installation to win, got %q", all[0].InstallationID)
	}
}

func TestOpenStorePersistsDevices(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "devices.json")

	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	if _, err := store.Upsert(RegisterInput{
		InstallationID: "phone-a",
		PushToken:      "aabbccdd",
	}); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	reopened, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("reopen OpenStore returned error: %v", err)
	}

	all := reopened.All()
	if len(all) != 1 {
		t.Fatalf("All returned %d devices, want 1", len(all))
	}

	if all[0].PushToken != "aabbccdd" {
		t.Fatalf("reopened wrong token: got %q", all[0].PushToken)
	}
}

func TestAllForChannelFiltersDevices(t *testing.T) {
	store := NewStore()

	if _, err := store.Upsert(RegisterInput{
		InstallationID: "phone-a",
		ChannelID:      "channel-a",
		PushToken:      "aabbccdd",
	}); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	if _, err := store.Upsert(RegisterInput{
		InstallationID: "phone-b",
		ChannelID:      "channel-b",
		PushToken:      "bbccddaa",
	}); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	channelA := store.AllForChannel("channel-a")
	if len(channelA) != 1 {
		t.Fatalf("AllForChannel returned %d devices, want 1", len(channelA))
	}

	if channelA[0].InstallationID != "phone-a" {
		t.Fatalf("wrong device returned for channel-a: got %q", channelA[0].InstallationID)
	}
}

func TestUpsertAcceptsWatchOSPlatform(t *testing.T) {
	store := NewStore()

	device, err := store.Upsert(RegisterInput{
		InstallationID: "watch-a",
		Platform:       "watchOS",
		PushToken:      "aabbccdd",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	if device.Platform != "watchos" {
		t.Fatalf("wrong platform stored: got %q", device.Platform)
	}
}
