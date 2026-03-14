package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEventForAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action   string
		wantType string
	}{
		{action: "stop", wantType: "completed"},
		{action: "completed", wantType: "completed"},
		{action: "subagent_stop", wantType: "subagent_completed"},
		{action: "subagent_completed", wantType: "subagent_completed"},
		{action: "failed", wantType: "failed"},
		{action: "permission_prompt", wantType: "attention"},
		{action: "idle_prompt", wantType: "attention"},
		{action: "attention", wantType: "attention"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.action, func(t *testing.T) {
			t.Parallel()
			got, err := eventForAction(tc.action)
			if err != nil {
				t.Fatalf("eventForAction(%q) returned error: %v", tc.action, err)
			}
			if got.Type != tc.wantType {
				t.Fatalf("eventForAction(%q) type=%q, want %q", tc.action, got.Type, tc.wantType)
			}
			if got.Source != "claude-code" {
				t.Fatalf("eventForAction(%q) source=%q, want %q", tc.action, got.Source, "claude-code")
			}
		})
	}

	if _, err := eventForAction("unknown"); err == nil {
		t.Fatalf("eventForAction(unknown) expected error")
	}
}

func TestMergeClaudeHooksIdempotentAndPrunesLegacy(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"hooks": map[string]any{
			"Notification": []any{
				map[string]any{
					"matcher": "legacy",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": `/bin/sh "${HOME}/Library/Application Support/AgentWatch/bin/watch-hook" stop`,
						},
						map[string]any{
							"type":    "command",
							"command": `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" attention`,
						},
					},
				},
			},
		},
	}

	if err := mergeClaudeHooks(config, true); err != nil {
		t.Fatalf("mergeClaudeHooks first run failed: %v", err)
	}
	first, _ := json.Marshal(config)

	if err := mergeClaudeHooks(config, true); err != nil {
		t.Fatalf("mergeClaudeHooks second run failed: %v", err)
	}
	second, _ := json.Marshal(config)
	if !bytes.Equal(first, second) {
		t.Fatalf("mergeClaudeHooks is not idempotent:\nfirst=%s\nsecond=%s", string(first), string(second))
	}

	raw, _ := json.Marshal(config)
	if strings.Contains(strings.ToLower(string(raw)), "watch-hook") {
		t.Fatalf("legacy watch-hook command was not pruned: %s", string(raw))
	}
	if !strings.Contains(string(raw), `"SubagentStop"`) {
		t.Fatalf("SubagentStop hooks not added: %s", string(raw))
	}
	if !strings.Contains(string(raw), `"permission_prompt"`) {
		t.Fatalf("Notification hooks not added: %s", string(raw))
	}
}

func TestMergeClaudeHooksInvalidHooksType(t *testing.T) {
	t.Parallel()

	config := map[string]any{
		"hooks": []any{},
	}
	if err := mergeClaudeHooks(config, true); err == nil {
		t.Fatalf("expected error for invalid hooks type")
	}
}

func TestResolveSettingsPath(t *testing.T) {
	t.Parallel()

	home := "/tmp/home"
	cwd := "/tmp/repo"

	userPath, err := resolveSettingsPath("user", cwd, home)
	if err != nil {
		t.Fatalf("resolveSettingsPath(user) failed: %v", err)
	}
	wantUser := filepath.Join(home, ".claude", "settings.json")
	if userPath != wantUser {
		t.Fatalf("resolveSettingsPath(user)=%q want %q", userPath, wantUser)
	}

	projectPath, err := resolveSettingsPath("project", cwd, home)
	if err != nil {
		t.Fatalf("resolveSettingsPath(project) failed: %v", err)
	}
	wantProject := filepath.Join(cwd, ".claude", "settings.json")
	if projectPath != wantProject {
		t.Fatalf("resolveSettingsPath(project)=%q want %q", projectPath, wantProject)
	}

	if _, err := resolveSettingsPath("bad", cwd, home); err == nil {
		t.Fatalf("resolveSettingsPath(bad) expected error")
	}
}

func TestPatchSettingsAtPathRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "settings.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write malformed JSON: %v", err)
	}

	if err := patchSettingsAtPath(path, true); err == nil {
		t.Fatalf("patchSettingsAtPath expected error on malformed JSON")
	}
}

func TestCreateOrRestoreInstallationFallsBackToCreate(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/claude/installations" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		mu.Lock()
		callCount++
		mu.Unlock()

		auth := r.Header.Get("Authorization")
		if auth == "Bearer stale-token" {
			http.Error(w, "stale", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"installationToken":"inst-token","installationId":"inst-id","claudeSessionToken":"claude-token"}`))
	}))
	defer server.Close()

	resp, err := createOrRestoreInstallation(server.Client(), server.URL, "stale-token")
	if err != nil {
		t.Fatalf("createOrRestoreInstallation failed: %v", err)
	}
	if resp.InstallationToken != "inst-token" {
		t.Fatalf("installationToken=%q want %q", resp.InstallationToken, "inst-token")
	}
	mu.Lock()
	gotCalls := callCount
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("callCount=%d want 2 (restore then create)", gotCalls)
	}
}

func TestSmokeAgainstBaseURL(t *testing.T) {
	t.Parallel()

	const (
		installationToken = "inst-token"
		claudeToken       = "claude-token"
		watchToken        = "watch-token"
		pairingCode       = "1234"
	)

	var (
		mu     sync.Mutex
		events []eventPayload
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/claude/installations" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"installationToken":"` + installationToken + `","installationId":"inst-id","claudeSessionToken":"` + claudeToken + `"}`))
			return
		case r.URL.Path == "/v1/watch/pairings/code" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer "+installationToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":"` + pairingCode + `"}`))
			return
		case r.URL.Path == "/v1/watch/pairings/claim" && r.Method == http.MethodPost:
			var body struct {
				Code string `json:"code"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Code != pairingCode {
				http.Error(w, "invalid code", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"watchSessionToken":"` + watchToken + `"}`))
			return
		case r.URL.Path == "/v1/events" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer "+claudeToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var payload eventPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			mu.Lock()
			events = append(events, payload)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"event":{"type":"completed"}}`))
			return
		case r.URL.Path == "/v1/events" && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer "+watchToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"events": events})
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := smokeAgainstBaseURL(server.Client(), server.URL); err != nil {
		t.Fatalf("smokeAgainstBaseURL failed: %v", err)
	}
}

func TestFormatPairingCodeDisplayANSI(t *testing.T) {
	t.Parallel()

	lines := formatPairingCodeDisplay("1234", true)
	if len(lines) != 2 {
		t.Fatalf("line count=%d want 2", len(lines))
	}
	if lines[0] != "Enter this 4-digit pairing code on your Apple Watch:" {
		t.Fatalf("unexpected heading: %q", lines[0])
	}
	if !strings.Contains(lines[1], "1 2 3 4") {
		t.Fatalf("formatted code missing spacing: %q", lines[1])
	}
	if !strings.Contains(lines[1], "\x1b[1;97;44m") || !strings.Contains(lines[1], "\x1b[0m") {
		t.Fatalf("formatted ANSI line missing escape codes: %q", lines[1])
	}
}

func TestFormatPairingCodeDisplayPlain(t *testing.T) {
	t.Parallel()

	lines := formatPairingCodeDisplay("9876", false)
	if len(lines) != 4 {
		t.Fatalf("line count=%d want 4", len(lines))
	}
	if lines[0] != "Enter this 4-digit pairing code on your Apple Watch:" {
		t.Fatalf("unexpected heading: %q", lines[0])
	}
	if lines[1] != "===========" {
		t.Fatalf("unexpected border: %q", lines[1])
	}
	if lines[2] != "| 9 8 7 6 |" {
		t.Fatalf("unexpected body: %q", lines[2])
	}
	if lines[3] != "===========" {
		t.Fatalf("unexpected border: %q", lines[3])
	}
}

func TestFormatPairingCodeDisplayEmpty(t *testing.T) {
	t.Parallel()

	lines := formatPairingCodeDisplay("   ", true)
	if len(lines) != 0 {
		t.Fatalf("line count=%d want 0", len(lines))
	}
}

func TestSpaceSeparatedCode(t *testing.T) {
	t.Parallel()

	if got := spaceSeparatedCode("1234"); got != "1 2 3 4" {
		t.Fatalf("spaceSeparatedCode(1234)=%q want %q", got, "1 2 3 4")
	}
	if got := spaceSeparatedCode(""); got != "" {
		t.Fatalf("spaceSeparatedCode(empty)=%q want empty", got)
	}
}
