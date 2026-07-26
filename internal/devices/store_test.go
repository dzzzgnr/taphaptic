package devices

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertPersistsAndRotatesToken(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "devices.json")
	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Upsert(Device{
		InstallationID:      "installation-a",
		WatchInstallationID: "watch-a",
		Token:               strings.Repeat("ab", 32),
		Environment:         "development",
		Topic:               "com.example.taphaptic",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Upsert(Device{
		InstallationID:      "installation-a",
		WatchInstallationID: "watch-a",
		Token:               strings.Repeat("cd", 32),
		Environment:         "production",
		Topic:               "com.example.taphaptic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatedAt != second.CreatedAt {
		t.Fatalf("createdAt changed during token rotation")
	}

	reopened, err := OpenStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.ForInstallation("installation-a")
	if len(got) != 1 || got[0].Token != strings.Repeat("cd", 32) {
		t.Fatalf("unexpected reloaded devices: %+v", got)
	}
}

func TestRejectsInvalidToken(t *testing.T) {
	store := NewStore()
	_, err := store.Upsert(Device{
		InstallationID:      "installation-a",
		WatchInstallationID: "watch-a",
		Token:               "not-a-token",
		Topic:               "com.example.taphaptic",
	})
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
}

func TestRemoveIsScopedToInstallation(t *testing.T) {
	store := NewStore()
	_, err := store.Upsert(Device{
		InstallationID:      "installation-a",
		WatchInstallationID: "watch-a",
		Token:               strings.Repeat("ab", 32),
		Topic:               "com.example.taphaptic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("installation-b", "watch-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if len(store.ForInstallation("installation-a")) != 1 {
		t.Fatalf("device was removed by another installation")
	}
}
