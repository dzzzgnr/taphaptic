package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentwatch/internal/channels"
	"agentwatch/internal/events"
	"agentwatch/internal/installations"
	"agentwatch/internal/watchpairings"
)

func TestHealthzDoesNotRequireAuth(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("GET /healthz returned %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestCreateInstallationReturnsScopedTokens(t *testing.T) {
	handler := newTestHandler()

	installation := createInstallation(t, handler, "")
	if installation.InstallationToken == "" {
		t.Fatalf("installation token is empty")
	}
	if installation.InstallationID == "" {
		t.Fatalf("installation ID is empty")
	}
	if installation.ChannelID == "" {
		t.Fatalf("channel ID is empty")
	}
	if installation.ClaudeSessionToken == "" {
		t.Fatalf("claude session token is empty")
	}
}

func TestCreateInstallationWithExistingTokenRestoresIdentity(t *testing.T) {
	handler := newTestHandler()

	first := createInstallation(t, handler, "")
	second := createInstallation(t, handler, first.InstallationToken)

	if first.InstallationID != second.InstallationID {
		t.Fatalf("restored installation ID mismatch: got %s want %s", second.InstallationID, first.InstallationID)
	}
	if first.InstallationToken != second.InstallationToken {
		t.Fatalf("restored installation token mismatch")
	}
	if first.ChannelID != second.ChannelID {
		t.Fatalf("restored channel ID mismatch")
	}
}

func TestWatchPairingAndEventFlow(t *testing.T) {
	handler := newTestHandler()

	installation := createInstallation(t, handler, "")
	watchCode := createWatchCode(t, handler, installation.InstallationToken)
	claim := claimWatchCode(t, handler, watchCode.Code, "watch-a")

	createEvent(t, handler, installation.ClaudeSessionToken, `{"type":"completed"}`)
	eventResponse := getEvents(t, handler, claim.WatchSessionToken, 0)

	if len(eventResponse.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(eventResponse.Events))
	}
	if eventResponse.Events[0].Type != events.TypeCompleted {
		t.Fatalf("unexpected event type: got %s", eventResponse.Events[0].Type)
	}
}

func TestWatchCodeClaimCannotBeReused(t *testing.T) {
	handler := newTestHandler()

	installation := createInstallation(t, handler, "")
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
		t.Fatalf("second claim returned %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestWatchTokenCannotCreateEvents(t *testing.T) {
	handler := newTestHandler()

	installation := createInstallation(t, handler, "")
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

func TestClaudeTokenCannotReadEvents(t *testing.T) {
	handler := newTestHandler()

	installation := createInstallation(t, handler, "")
	request := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	request.Header.Set("Authorization", "Bearer "+installation.ClaudeSessionToken)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("claude token GET /v1/events returned %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestWatchCodeEndpointRequiresInstallationToken(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodPost, "/v1/watch/pairings/code", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("POST /v1/watch/pairings/code returned %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestWatchClaimRejectsNonNumericCode(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodPost, "/v1/watch/pairings/claim", bytes.NewBufferString(`{"code":"12ab","watchInstallationId":"watch-a"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("POST /v1/watch/pairings/claim returned %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

type installationResponse struct {
	InstallationToken string `json:"installationToken"`
	InstallationID    string `json:"installationId"`
	ChannelID         string `json:"channelId"`
	ClaudeSessionToken string `json:"claudeSessionToken"`
}

type watchCodeResponse struct {
	Code   string `json:"code"`
	CodeID string `json:"codeId"`
}

type watchClaimResponse struct {
	WatchSessionToken string `json:"watchSessionToken"`
	ChannelID         string `json:"channelId"`
}

type eventListResponse struct {
	Events []events.Event `json:"events"`
}

func newTestHandler() *Handler {
	return NewHandler(Config{
		Store:             events.NewStore(64),
		ChannelStore:      channels.NewStore(),
		InstallationStore: installations.NewStore(),
		WatchPairingStore: watchpairings.NewStore(),
	})
}

func createInstallation(t *testing.T, handler *Handler, installationToken string) installationResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/claude/installations", bytes.NewBufferString(`{}`))
	if installationToken != "" {
		request.Header.Set("Authorization", "Bearer "+installationToken)
	}
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/claude/installations returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response installationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode installation response: %v", err)
	}
	return response
}

func createWatchCode(t *testing.T, handler *Handler, installationToken string) watchCodeResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/watch/pairings/code", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer "+installationToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/watch/pairings/code returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response watchCodeResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode watch code response: %v", err)
	}
	if response.Code == "" || response.CodeID == "" {
		t.Fatalf("watch code response is incomplete: %+v", response)
	}
	return response
}

func claimWatchCode(t *testing.T, handler *Handler, code string, watchInstallationID string) watchClaimResponse {
	t.Helper()

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/watch/pairings/claim",
		bytes.NewBufferString(`{"code":"`+code+`","watchInstallationId":"`+watchInstallationID+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/watch/pairings/claim returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response watchClaimResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode watch claim response: %v", err)
	}
	if response.WatchSessionToken == "" {
		t.Fatalf("watch claim response missing watchSessionToken")
	}
	if response.ChannelID == "" {
		t.Fatalf("watch claim response missing channelId")
	}
	return response
}

func createEvent(t *testing.T, handler *Handler, claudeSessionToken string, payload string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewBufferString(payload))
	request.Header.Set("Authorization", "Bearer "+claudeSessionToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /v1/events returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func getEvents(t *testing.T, handler *Handler, watchSessionToken string, since int64) eventListResponse {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/v1/events?since=0", nil)
	request.Header.Set("Authorization", "Bearer "+watchSessionToken)
	recorder := httptest.NewRecorder()
	handler.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /v1/events returned %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response eventListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	return response
}
