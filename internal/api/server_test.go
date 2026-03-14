package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"agentwatch/internal/channels"
	"agentwatch/internal/devices"
	"agentwatch/internal/events"
	"agentwatch/internal/installations"
	"agentwatch/internal/pairings"
	"agentwatch/internal/push"
	"agentwatch/internal/sessions"
	"agentwatch/internal/watchpairings"
)

type recordingNotifier struct {
	calls   int
	event   events.Event
	devices []devices.Device
}

func (n *recordingNotifier) NotifyEvent(_ context.Context, event events.Event, devicesList []devices.Device) error {
	n.calls++
	n.event = event
	n.devices = append([]devices.Device(nil), devicesList...)
	return nil
}

func TestHealthzDoesNotRequireAuth(t *testing.T) {
	handler := newTestHandler(nil)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("GET /healthz returned %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestCreateEventRequiresBearerToken(t *testing.T) {
	handler := newTestHandler(nil)

	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewBufferString(`{"type":"completed"}`))
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("POST /v1/events returned %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestPairingFlowReturnsScopedTokens(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	pairing := createPairing(t, handler, installation.InstallationToken)

	pending := getPairingStatus(t, handler, installation.InstallationToken, pairing.PairingID)
	if pending.Status != pairings.StatusPending {
		t.Fatalf("pending status mismatch: got %q want %q", pending.Status, pairings.StatusPending)
	}

	claim := claimPairing(t, handler, pairing.PairingToken, "phone-a", "aabbccdd")
	if claim.PhoneSessionToken == "" {
		t.Fatalf("claim response missing phoneSessionToken")
	}

	paired := getPairingStatus(t, handler, installation.InstallationToken, pairing.PairingID)
	if paired.Status != pairings.StatusPaired {
		t.Fatalf("paired status mismatch: got %q want %q", paired.Status, pairings.StatusPaired)
	}
	if paired.ClaudeSessionToken == "" {
		t.Fatalf("paired response missing claudeSessionToken")
	}
	if paired.ChannelID == "" {
		t.Fatalf("paired response missing channelID")
	}

	createEvent(t, handler, paired.ClaudeSessionToken, `{"type":"completed"}`)
	status := getStatus(t, handler, claim.PhoneSessionToken)
	if status.Current == nil {
		t.Fatalf("expected current event for paired phone token")
	}
	if status.Current.Type != events.TypeCompleted {
		t.Fatalf("wrong event type in status: got %s", status.Current.Type)
	}
}

func TestChannelIsolationBetweenPairings(t *testing.T) {
	handler := newTestHandler(nil)

	installationA := createInstallation(t, handler)
	pairingA := createPairing(t, handler, installationA.InstallationToken)
	claimA := claimPairing(t, handler, pairingA.PairingToken, "phone-a", "aabbccdd")
	pairedA := getPairingStatus(t, handler, installationA.InstallationToken, pairingA.PairingID)

	installationB := createInstallation(t, handler)
	pairingB := createPairing(t, handler, installationB.InstallationToken)
	claimB := claimPairing(t, handler, pairingB.PairingToken, "phone-b", "bbccddaa")

	createEvent(t, handler, pairedA.ClaudeSessionToken, `{"type":"failed"}`)

	statusA := getStatus(t, handler, claimA.PhoneSessionToken)
	if statusA.Current == nil || statusA.Current.Type != events.TypeFailed {
		t.Fatalf("status A did not receive its channel event: %+v", statusA.Current)
	}

	statusB := getStatus(t, handler, claimB.PhoneSessionToken)
	if statusB.Current != nil {
		t.Fatalf("status B should not receive channel A event, got %+v", statusB.Current)
	}
}

func TestPairingClaimCannotBeReused(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	pairing := createPairing(t, handler, installation.InstallationToken)
	_ = claimPairing(t, handler, pairing.PairingToken, "phone-a", "aabbccdd")

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/pairings/claim",
		bytes.NewBufferString(`{"pairingToken":"`+pairing.PairingToken+`","phoneInstallationId":"phone-b","pushToken":"bbccddaa"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("second claim returned %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestWatchCodePairingFlowReturnsWatchToken(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	if installation.ClaudeSessionToken == "" {
		t.Fatalf("installation response missing claudeSessionToken")
	}

	watchCode := createWatchCode(t, handler, installation.InstallationToken)
	claim := claimWatchCode(t, handler, watchCode.Code, "watch-a")
	if claim.WatchSessionToken == "" {
		t.Fatalf("claim response missing watchSessionToken")
	}
	if claim.ChannelID == "" {
		t.Fatalf("claim response missing channelID")
	}

	createEvent(t, handler, installation.ClaudeSessionToken, `{"type":"completed"}`)

	status := getStatus(t, handler, claim.WatchSessionToken)
	if status.Current == nil {
		t.Fatalf("expected current event for watch token")
	}
	if status.Current.Type != events.TypeCompleted {
		t.Fatalf("wrong event type in status: got %s", status.Current.Type)
	}
}

func TestWatchTokenReceivesSubagentCompletedEvent(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	watchCode := createWatchCode(t, handler, installation.InstallationToken)
	claim := claimWatchCode(t, handler, watchCode.Code, "watch-a")

	createEvent(t, handler, installation.ClaudeSessionToken, `{"type":"subagent_completed"}`)

	status := getStatus(t, handler, claim.WatchSessionToken)
	if status.Current == nil {
		t.Fatalf("expected current event for watch token")
	}
	if status.Current.Type != events.TypeSubagentCompleted {
		t.Fatalf("wrong event type in status: got %s", status.Current.Type)
	}
}

func TestWatchCodeClaimCannotBeReused(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	watchCode := createWatchCode(t, handler, installation.InstallationToken)
	_ = claimWatchCode(t, handler, watchCode.Code, "watch-a")

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/watch/pairings/claim",
		bytes.NewBufferString(`{"code":"`+watchCode.Code+`","watchInstallationId":"watch-b"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("second watch claim returned %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestWatchTokenCannotCreateEvents(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	watchCode := createWatchCode(t, handler, installation.InstallationToken)
	claim := claimWatchCode(t, handler, watchCode.Code, "watch-a")

	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewBufferString(`{"type":"completed"}`))
	request.Header.Set("Authorization", "Bearer "+claim.WatchSessionToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("watch token POST /v1/events returned %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestClaudeTokenCannotReadStatus(t *testing.T) {
	handler := newTestHandler(nil)

	installation := createInstallation(t, handler)
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer "+installation.ClaudeSessionToken)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("claude token GET /v1/status returned %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestLegacyLoginStillWorks(t *testing.T) {
	handler := newTestHandler(nil)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewBufferString(`{"code":"login-code"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/auth/login returned %d, want %d", recorder.Code, http.StatusOK)
	}

	var response struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SessionToken == "" {
		t.Fatalf("expected non-empty legacy session token")
	}
}

func TestBuildTerminalQRIsCompact(t *testing.T) {
	qr := buildTerminalQR("https://pairagentwatchapp.vercel.app/p/" + strings.Repeat("a", 64))
	if strings.TrimSpace(qr) == "" {
		t.Fatalf("buildTerminalQR returned empty output")
	}

	lines := strings.Split(qr, "\n")
	if len(lines) > 40 {
		t.Fatalf("buildTerminalQR produced %d lines, want <= 40", len(lines))
	}

	maxWidth := 0
	hasDarkModules := false
	for _, line := range lines {
		width := utf8.RuneCountInString(line)
		if width > maxWidth {
			maxWidth = width
		}
		if strings.ContainsAny(line, "█▀▄") {
			hasDarkModules = true
		}
	}

	if maxWidth > 80 {
		t.Fatalf("buildTerminalQR produced width %d, want <= 80", maxWidth)
	}
	if !hasDarkModules {
		t.Fatalf("buildTerminalQR output does not contain dark module characters")
	}
}

func TestNormalizePairingToken(t *testing.T) {
	raw := "bc8136aa%20%20%2008bf97d7afd4aa2cb905ed54b831b6bab4594fcb"
	normalized := normalizePairingToken(raw)
	want := "bc8136aa08bf97d7afd4aa2cb905ed54b831b6bab4594fcb"
	if normalized != want {
		t.Fatalf("normalizePairingToken(%q) = %q, want %q", raw, normalized, want)
	}
}

func newTestHandler(notifier *recordingNotifier) *Handler {
	var pushNotifier push.Notifier
	if notifier != nil {
		pushNotifier = notifier
	}

	return NewHandler(Config{
		APIKey:            "admin-key",
		LoginSecret:       "login-code",
		Store:             events.NewStore(64),
		DeviceStore:       devices.NewStore(),
		Notifier:          pushNotifier,
		PushEnabled:       true,
		SessionStore:      sessions.NewStore(),
		ChannelStore:      channels.NewStore(),
		InstallationStore: installations.NewStore(),
		PairingStore:      pairings.NewStore(),
		WatchPairingStore: watchpairings.NewStore(),
	})
}

func createInstallation(t *testing.T, handler *Handler) createInstallationResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/claude/installations", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/claude/installations returned %d, want %d", recorder.Code, http.StatusOK)
	}

	var response createInstallationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create installation response: %v", err)
	}
	if response.InstallationToken == "" || response.InstallationID == "" {
		t.Fatalf("invalid installation response: %+v", response)
	}
	return response
}

func createPairing(t *testing.T, handler *Handler, installationToken string) createPairingResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/pairings", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer "+installationToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/pairings returned %d, want %d", recorder.Code, http.StatusOK)
	}

	var response createPairingResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create pairing response: %v", err)
	}
	if response.PairingID == "" || response.PairingToken == "" {
		t.Fatalf("invalid pairing response: %+v", response)
	}
	return response
}

func createWatchCode(t *testing.T, handler *Handler, installationToken string) createWatchPairingCodeResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/watch/pairings/code", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer "+installationToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/watch/pairings/code returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response createWatchPairingCodeResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create watch code response: %v", err)
	}
	if response.Code == "" || response.CodeID == "" {
		t.Fatalf("invalid watch code response: %+v", response)
	}
	return response
}

func claimWatchCode(t *testing.T, handler *Handler, code string, watchInstallationID string) claimWatchPairingResponse {
	t.Helper()

	payload := `{"code":"` + code + `","watchInstallationId":"` + watchInstallationID + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/watch/pairings/claim", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/watch/pairings/claim returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response claimWatchPairingResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim watch code response: %v", err)
	}
	return response
}

func getPairingStatus(t *testing.T, handler *Handler, installationToken string, pairingID string) pairingStatusResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/pairings/"+pairingID, nil)
	request.SetPathValue("pairingID", pairingID)
	request.Header.Set("Authorization", "Bearer "+installationToken)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/pairings/%s returned %d, want %d", pairingID, recorder.Code, http.StatusOK)
	}

	var response pairingStatusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode pairing status response: %v", err)
	}
	return response
}

func claimPairing(t *testing.T, handler *Handler, pairingToken string, phoneInstallationID string, pushToken string) claimPairingResponse {
	t.Helper()

	payload := `{"pairingToken":"` + pairingToken + `","phoneInstallationId":"` + phoneInstallationID + `","pushToken":"` + pushToken + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/pairings/claim", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/pairings/claim returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response claimPairingResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim pairing response: %v", err)
	}
	return response
}

func createEvent(t *testing.T, handler *Handler, claudeToken string, payload string) createEventResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewBufferString(payload))
	request.Header.Set("Authorization", "Bearer "+claudeToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/events returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response createEventResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create event response: %v", err)
	}
	return response
}

func getStatus(t *testing.T, handler *Handler, phoneToken string) statusResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer "+phoneToken)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/status returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response statusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	return response
}
