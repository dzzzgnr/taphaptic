package installations

import (
	"path/filepath"
	"testing"
)

func TestCreateAndTouchTokenPersist(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "claude-installations.json")

	store, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	installation, err := store.Create()
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if installation.ID == "" {
		t.Fatalf("installation ID is empty")
	}
	if installation.Token == "" {
		t.Fatalf("installation token is empty")
	}

	if !store.TouchToken(installation.Token) {
		t.Fatalf("TouchToken returned false")
	}

	reopened, err := OpenStore(statePath)
	if err != nil {
		t.Fatalf("reopen OpenStore returned error: %v", err)
	}

	reloaded, ok := reopened.GetByToken(installation.Token)
	if !ok {
		t.Fatalf("GetByToken returned false after reopen")
	}
	if reloaded.ID != installation.ID {
		t.Fatalf("reloaded installation mismatch: got %q want %q", reloaded.ID, installation.ID)
	}
}
