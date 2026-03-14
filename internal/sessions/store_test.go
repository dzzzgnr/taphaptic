package sessions

import (
	"path/filepath"
	"testing"
)

func TestCreateAndTouchPersist(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "sessions.json")

	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	session, err := store.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if session.Token == "" {
		t.Fatalf("Create returned empty token")
	}

	if !store.Touch(session.Token) {
		t.Fatalf("Touch returned false for created token")
	}

	reopened, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("reopen OpenStore returned error: %v", err)
	}

	if !reopened.Touch(session.Token) {
		t.Fatalf("Touch returned false after reopen")
	}
}
