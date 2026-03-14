package channels

import (
	"path/filepath"
	"testing"
)

func TestEnsureForInstallationPersists(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "channels.json")

	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	channel, err := store.EnsureForInstallation("claude-installation-a")
	if err != nil {
		t.Fatalf("EnsureForInstallation returned error: %v", err)
	}

	if channel.ID == "" {
		t.Fatalf("channel ID is empty")
	}
	if channel.ClaudeSessionToken == "" {
		t.Fatalf("Claude session token is empty")
	}

	reopened, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("reopen OpenStore returned error: %v", err)
	}

	reloaded, ok := reopened.GetByInstallation("claude-installation-a")
	if !ok {
		t.Fatalf("GetByInstallation returned false after reopen")
	}
	if reloaded.ID != channel.ID {
		t.Fatalf("reloaded channel mismatch: got %q want %q", reloaded.ID, channel.ID)
	}
}

func TestRotatePhoneSessionReplacesOldToken(t *testing.T) {
	store := NewStore()

	channel, err := store.EnsureForInstallation("claude-installation-a")
	if err != nil {
		t.Fatalf("EnsureForInstallation returned error: %v", err)
	}

	paired, err := store.RotatePhoneSession(channel.InstallationID)
	if err != nil {
		t.Fatalf("RotatePhoneSession returned error: %v", err)
	}
	if paired.PhoneSessionToken == "" {
		t.Fatalf("Phone session token is empty")
	}

	previousPhoneToken := paired.PhoneSessionToken
	rotated, err := store.RotatePhoneSession(channel.InstallationID)
	if err != nil {
		t.Fatalf("RotatePhoneSession second call returned error: %v", err)
	}

	if rotated.PhoneSessionToken == previousPhoneToken {
		t.Fatalf("expected phone token to rotate")
	}

	if _, ok := store.GetByPhoneToken(previousPhoneToken); ok {
		t.Fatalf("old phone token still resolves to a channel")
	}
}

func TestRotateWatchSessionReplacesOldToken(t *testing.T) {
	store := NewStore()

	channel, err := store.EnsureForInstallation("claude-installation-a")
	if err != nil {
		t.Fatalf("EnsureForInstallation returned error: %v", err)
	}

	paired, err := store.RotateWatchSession(channel.InstallationID)
	if err != nil {
		t.Fatalf("RotateWatchSession returned error: %v", err)
	}
	if paired.WatchSessionToken == "" {
		t.Fatalf("Watch session token is empty")
	}

	previousWatchToken := paired.WatchSessionToken
	rotated, err := store.RotateWatchSession(channel.InstallationID)
	if err != nil {
		t.Fatalf("RotateWatchSession second call returned error: %v", err)
	}

	if rotated.WatchSessionToken == previousWatchToken {
		t.Fatalf("expected watch token to rotate")
	}

	if _, ok := store.GetByWatchToken(previousWatchToken); ok {
		t.Fatalf("old watch token still resolves to a channel")
	}
}
