package pairings

import (
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestCreateAndClaimByToken(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "pairings.json")

	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	pairing, err := store.Create("install-a", 2*time.Minute)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if pairing.ID == "" || pairing.Token == "" {
		t.Fatalf("pairing identifiers should be non-empty")
	}

	claimed, err := store.ClaimByToken(pairing.Token, "channel-a")
	if err != nil {
		t.Fatalf("ClaimByToken returned error: %v", err)
	}
	if claimed.ChannelID != "channel-a" {
		t.Fatalf("ClaimByToken returned wrong channel: got %q", claimed.ChannelID)
	}
	if claimed.ClaimedAt.IsZero() {
		t.Fatalf("ClaimedAt should be set")
	}

	_, err = store.ClaimByToken(pairing.Token, "channel-a")
	if err != ErrAlreadyPaired {
		t.Fatalf("second claim returned %v, want ErrAlreadyPaired", err)
	}
}

func TestClaimByTokenRejectsExpiredPairing(t *testing.T) {
	store := NewStore()

	pairing, err := store.Create("install-a", time.Millisecond)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	_, err = store.ClaimByToken(pairing.Token, "channel-a")
	if err != ErrExpired {
		t.Fatalf("ClaimByToken returned %v, want ErrExpired", err)
	}
}

func TestCreateUsesURLSafePairingIdentifiers(t *testing.T) {
	store := NewStore()

	pairing, err := store.Create("install-a", 2*time.Minute)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	pattern := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	if !pattern.MatchString(pairing.ID) {
		t.Fatalf("pairing ID is not URL-safe: %q", pairing.ID)
	}
	if !pattern.MatchString(pairing.Token) {
		t.Fatalf("pairing token is not URL-safe: %q", pairing.Token)
	}
}
