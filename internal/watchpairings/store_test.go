package watchpairings

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateAndClaimCode(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "watch_pairings.json")

	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	pairing, code, err := store.Create("install-a", "channel-a", 2*time.Minute)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if pairing.ID == "" || code == "" {
		t.Fatalf("Create returned empty values: %+v code=%q", pairing, code)
	}
	if len(code) != 6 {
		t.Fatalf("Create returned wrong code length: %d", len(code))
	}
	if strings.TrimSpace(pairing.CodeHash) == "" {
		t.Fatalf("Create returned empty code hash")
	}

	claimed, err := store.Claim(code)
	if err != nil {
		t.Fatalf("Claim returned error: %v", err)
	}
	if claimed.ID != pairing.ID {
		t.Fatalf("Claim returned wrong ID: got %q want %q", claimed.ID, pairing.ID)
	}
	if claimed.ChannelID != "channel-a" {
		t.Fatalf("Claim returned wrong channel: got %q", claimed.ChannelID)
	}
	if claimed.ClaimedAt.IsZero() {
		t.Fatalf("Claim should set claimedAt")
	}

	_, err = store.Claim(code)
	if err != ErrAlreadyClaimed {
		t.Fatalf("second Claim returned %v, want %v", err, ErrAlreadyClaimed)
	}
}

func TestClaimExpiredCode(t *testing.T) {
	store := NewStore()

	_, code, err := store.Create("install-a", "channel-a", time.Millisecond)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	_, err = store.Claim(code)
	if err != ErrExpired {
		t.Fatalf("Claim returned %v, want %v", err, ErrExpired)
	}
}

func TestCreateInvalidatesOlderPendingCodeForInstallation(t *testing.T) {
	store := NewStore()

	_, firstCode, err := store.Create("install-a", "channel-a", 2*time.Minute)
	if err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}

	_, secondCode, err := store.Create("install-a", "channel-a", 2*time.Minute)
	if err != nil {
		t.Fatalf("second Create returned error: %v", err)
	}

	if firstCode == secondCode {
		t.Fatalf("expected generated codes to differ")
	}

	_, err = store.Claim(firstCode)
	if err != ErrNotFound {
		t.Fatalf("Claim for invalidated code returned %v, want %v", err, ErrNotFound)
	}

	if _, err := store.Claim(secondCode); err != nil {
		t.Fatalf("Claim for latest code returned error: %v", err)
	}
}

func TestClaimRejectsInvalidFormat(t *testing.T) {
	store := NewStore()

	if _, err := store.Claim("abcd12"); err != ErrInvalidCode {
		t.Fatalf("Claim returned %v, want %v", err, ErrInvalidCode)
	}
}

func TestCreatePrunesExpiredCodes(t *testing.T) {
	store := NewStore()

	_, firstCode, err := store.Create("install-a", "channel-a", time.Millisecond)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	if _, _, err := store.Create("install-b", "channel-b", 2*time.Minute); err != nil {
		t.Fatalf("second Create returned error: %v", err)
	}

	if _, err := store.Claim(firstCode); err != ErrNotFound {
		t.Fatalf("Claim for expired code returned %v, want %v", err, ErrNotFound)
	}

	if len(store.byID) != 1 {
		t.Fatalf("expected 1 active code after pruning, got %d", len(store.byID))
	}
}
