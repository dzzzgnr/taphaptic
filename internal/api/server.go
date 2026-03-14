package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentwatch/internal/channels"
	"agentwatch/internal/devices"
	"agentwatch/internal/events"
	"agentwatch/internal/installations"
	"agentwatch/internal/pairings"
	"agentwatch/internal/push"
	"agentwatch/internal/sessions"
	"agentwatch/internal/watchpairings"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	defaultPairingTTL           = 10 * time.Minute
	defaultRateLimitWindow      = time.Minute
	defaultCreateInstallPerIP   = 12
	defaultClaimPairingPerIP    = 60
	defaultWatchPollInterval    = 1
	defaultPairingBaseURL       = "https://pairagentwatchapp.vercel.app"
	defaultPublicBackendBaseURL = "https://agentwatch-api-production-39a1.up.railway.app"
)

type Config struct {
	APIKey             string
	LoginSecret        string
	PublicAPIBaseURL   string
	PairBaseURL        string
	Logger             *log.Logger
	Store              *events.Store
	DeviceStore        *devices.Store
	Notifier           push.Notifier
	PushEnabled        bool
	SessionStore       *sessions.Store
	ChannelStore       *channels.Store
	InstallationStore  *installations.Store
	PairingStore       *pairings.Store
	WatchPairingStore  *watchpairings.Store
	PairingTTL         time.Duration
	CreateInstallPerIP int
	ClaimPairingPerIP  int
}

type Handler struct {
	apiKey             string
	loginSecret        string
	publicAPIBaseURL   string
	pairBaseURL        string
	logger             *log.Logger
	store              *events.Store
	devices            *devices.Store
	notifier           push.Notifier
	pushEnabled        bool
	sessions           *sessions.Store
	channels           *channels.Store
	installations      *installations.Store
	pairings           *pairings.Store
	watchPairings      *watchpairings.Store
	pairingTTL         time.Duration
	createInstallPerIP int
	claimPairingPerIP  int
	limiter            *requestLimiter
}

type createEventRequest struct {
	Type      string `json:"type"`
	ChannelID string `json:"channelId,omitempty"`
	Source    string `json:"source,omitempty"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
}

type createEventResponse struct {
	Event events.Event `json:"event"`
}

type createDeviceRequest struct {
	InstallationID string `json:"installationId"`
	ChannelID      string `json:"channelId,omitempty"`
	Platform       string `json:"platform,omitempty"`
	PushToken      string `json:"pushToken"`
}

type createDeviceResponse struct {
	Device devices.Device `json:"device"`
}

type createLoginRequest struct {
	Code string `json:"code"`
}

type createLoginResponse struct {
	SessionToken string `json:"sessionToken"`
}

type createInstallationResponse struct {
	InstallationToken string    `json:"installationToken"`
	InstallationID    string    `json:"installationId"`
	ChannelID         string    `json:"channelId,omitempty"`
	ClaudeSessionToken string   `json:"claudeSessionToken,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

type createPairingRequest struct {
	InstallationID string `json:"installationId,omitempty"`
}

type createPairingResponse struct {
	PairingID    string    `json:"pairingId"`
	PairingToken string    `json:"pairingToken"`
	PairingURL   string    `json:"pairingURL"`
	TerminalQR   string    `json:"terminalQR"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type pairingStatusResponse struct {
	Status             pairings.Status `json:"status"`
	ExpiresAt          *time.Time      `json:"expiresAt,omitempty"`
	ClaudeSessionToken string          `json:"claudeSessionToken,omitempty"`
	ChannelID          string          `json:"channelId,omitempty"`
	PairedAt           *time.Time      `json:"pairedAt,omitempty"`
}

type claimPairingRequest struct {
	PairingToken        string `json:"pairingToken"`
	PhoneInstallationID string `json:"phoneInstallationId"`
	PushToken           string `json:"pushToken"`
}

type claimPairingResponse struct {
	PhoneSessionToken  string `json:"phoneSessionToken"`
	ChannelID          string `json:"channelId"`
	PollIntervalSecond int    `json:"pollIntervalSeconds"`
}

type createWatchPairingCodeRequest struct {
	InstallationID string `json:"installationId,omitempty"`
}

type createWatchPairingCodeResponse struct {
	Code      string    `json:"code"`
	CodeID    string    `json:"codeId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type claimWatchPairingRequest struct {
	Code                string `json:"code"`
	WatchInstallationID string `json:"watchInstallationId"`
	PushToken           string `json:"pushToken,omitempty"`
}

type claimWatchPairingResponse struct {
	WatchSessionToken  string `json:"watchSessionToken"`
	ChannelID          string `json:"channelId"`
	PollIntervalSecond int    `json:"pollIntervalSeconds"`
}

type eventsResponse struct {
	Events []events.Event `json:"events"`
}

type statusResponse struct {
	Current        *events.Event `json:"current,omitempty"`
	PushConfigured bool          `json:"pushConfigured"`
}

type authScope string

const (
	authScopeUnknown      authScope = "unknown"
	authScopeAdmin        authScope = "admin"
	authScopeInstallation authScope = "installation"
	authScopeClaude       authScope = "claude"
	authScopePhone        authScope = "phone"
	authScopeWatch        authScope = "watch"
	authScopeLegacy       authScope = "legacy"
)

type authContext struct {
	Scope          authScope
	Token          string
	ChannelID      string
	InstallationID string
}

type authContextKey struct{}

func NewHandler(cfg Config) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	notifier := cfg.Notifier
	if notifier == nil {
		notifier = push.NoopNotifier{}
	}

	pairingTTL := cfg.PairingTTL
	if pairingTTL <= 0 {
		pairingTTL = defaultPairingTTL
	}

	createInstallPerIP := cfg.CreateInstallPerIP
	if createInstallPerIP <= 0 {
		createInstallPerIP = defaultCreateInstallPerIP
	}

	claimPairingPerIP := cfg.ClaimPairingPerIP
	if claimPairingPerIP <= 0 {
		claimPairingPerIP = defaultClaimPairingPerIP
	}

	publicAPIBaseURL := strings.TrimSpace(cfg.PublicAPIBaseURL)
	if publicAPIBaseURL == "" {
		publicAPIBaseURL = defaultPublicBackendBaseURL
	}

	pairBaseURL := strings.TrimSpace(cfg.PairBaseURL)
	if pairBaseURL == "" {
		pairBaseURL = defaultPairingBaseURL
	}

	return &Handler{
		apiKey:             strings.TrimSpace(cfg.APIKey),
		loginSecret:        strings.TrimSpace(cfg.LoginSecret),
		publicAPIBaseURL:   strings.TrimRight(publicAPIBaseURL, "/"),
		pairBaseURL:        strings.TrimRight(pairBaseURL, "/"),
		logger:             logger,
		store:              cfg.Store,
		devices:            cfg.DeviceStore,
		notifier:           notifier,
		pushEnabled:        cfg.PushEnabled,
		sessions:           cfg.SessionStore,
		channels:           cfg.ChannelStore,
		installations:      cfg.InstallationStore,
		pairings:           cfg.PairingStore,
		watchPairings:      cfg.WatchPairingStore,
		pairingTTL:         pairingTTL,
		createInstallPerIP: createInstallPerIP,
		claimPairingPerIP:  claimPairingPerIP,
		limiter:            newRequestLimiter(),
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("POST /v1/auth/login", h.handleCreateLogin)
	mux.HandleFunc("POST /v1/claude/installations", h.handleCreateInstallation)
	mux.HandleFunc("POST /v1/pairings", h.handleCreatePairing)
	mux.HandleFunc("GET /v1/pairings/qr/{pairingToken}", h.handlePairingQR)
	mux.HandleFunc("GET /v1/pairings/{pairingID}", h.handleGetPairing)
	mux.HandleFunc("POST /v1/pairings/claim", h.handleClaimPairing)
	mux.HandleFunc("POST /v1/watch/pairings/code", h.handleCreateWatchPairingCode)
	mux.HandleFunc("POST /v1/watch/pairings/claim", h.handleClaimWatchPairingCode)
	mux.HandleFunc("POST /v1/events", h.handleCreateEvent)
	mux.HandleFunc("POST /v1/devices", h.handleCreateDevice)
	mux.HandleFunc("GET /v1/events", h.handleEvents)
	mux.HandleFunc("GET /v1/status", h.handleStatus)
	return h.withAuth(mux)
}

func (h *Handler) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.isPublicRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		auth := h.resolveAuth(r.Header.Get("Authorization"))
		if auth.Scope == authScopeUnknown {
			h.logger.Printf("api.auth_failed method=%s path=%s remote=%s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, auth)))
	})
}

func (h *Handler) isPublicRoute(path string) bool {
	if strings.HasPrefix(path, "/v1/pairings/qr/") {
		return true
	}

	switch path {
	case "/healthz", "/v1/auth/login", "/v1/claude/installations", "/v1/pairings/claim", "/v1/watch/pairings/claim":
		return true
	default:
		return false
	}
}

func (h *Handler) resolveAuth(authorizationHeader string) authContext {
	token, ok := bearerToken(authorizationHeader)
	if !ok {
		return authContext{Scope: authScopeUnknown}
	}

	if h.apiKey != "" && token == h.apiKey {
		return authContext{
			Scope: authScopeAdmin,
			Token: token,
		}
	}

	if h.installations != nil {
		if installation, found := h.installations.GetByToken(token); found {
			_ = h.installations.TouchToken(token)
			return authContext{
				Scope:          authScopeInstallation,
				Token:          token,
				InstallationID: installation.ID,
			}
		}
	}

	if h.channels != nil {
		if channel, found := h.channels.GetByClaudeToken(token); found {
			return authContext{
				Scope:          authScopeClaude,
				Token:          token,
				ChannelID:      channel.ID,
				InstallationID: channel.InstallationID,
			}
		}

		if channel, found := h.channels.GetByPhoneToken(token); found {
			return authContext{
				Scope:          authScopePhone,
				Token:          token,
				ChannelID:      channel.ID,
				InstallationID: channel.InstallationID,
			}
		}

		if channel, found := h.channels.GetByWatchToken(token); found {
			return authContext{
				Scope:          authScopeWatch,
				Token:          token,
				ChannelID:      channel.ID,
				InstallationID: channel.InstallationID,
			}
		}
	}

	if h.sessions != nil && h.sessions.Touch(token) {
		return authContext{
			Scope: authScopeLegacy,
			Token: token,
		}
	}

	return authContext{Scope: authScopeUnknown}
}

func (h *Handler) authFromContext(r *http.Request) authContext {
	auth, ok := r.Context().Value(authContextKey{}).(authContext)
	if !ok {
		return authContext{Scope: authScopeUnknown}
	}
	return auth
}

func (h *Handler) requireScope(r *http.Request, allowed ...authScope) (authContext, bool) {
	auth := h.authFromContext(r)
	for _, scope := range allowed {
		if auth.Scope == scope {
			return auth, true
		}
	}
	return authContext{}, false
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateLogin(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var payload createLoginRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(payload.Code) == "" || strings.TrimSpace(payload.Code) != h.loginSecret {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	if h.sessions == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	session, err := h.sessions.Create()
	if err != nil {
		h.logger.Printf("api.login_session_failed error=%v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("api.login_created legacy=true")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(createLoginResponse{SessionToken: session.Token}); err != nil {
		h.logger.Printf("api.login_encode_failed error=%v", err)
	}
}

func (h *Handler) handleCreateInstallation(w http.ResponseWriter, r *http.Request) {
	if h.installations == nil || h.channels == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	if !h.allowRequestFromIP(r, "installations:create", h.createInstallPerIP) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}

	if token, ok := bearerToken(r.Header.Get("Authorization")); ok {
		if installation, found := h.installations.GetByToken(token); found {
			channel, err := h.channels.EnsureForInstallation(installation.ID)
			if err != nil {
				h.logger.Printf("api.installation_channel_failed installation=%s error=%v", installation.ID, err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			_ = h.installations.TouchToken(token)
			h.writeJSON(w, createInstallationResponse{
				InstallationToken:  installation.Token,
				InstallationID:     installation.ID,
				ChannelID:          channel.ID,
				ClaudeSessionToken: channel.ClaudeSessionToken,
				CreatedAt:          installation.CreatedAt,
			})
			return
		}
	}

	installation, err := h.installations.Create()
	if err != nil {
		h.logger.Printf("api.installation_create_failed error=%v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	channel, err := h.channels.EnsureForInstallation(installation.ID)
	if err != nil {
		h.logger.Printf("api.installation_channel_failed installation=%s error=%v", installation.ID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("api.installation_created installation=%s", installation.ID)
	h.writeJSON(w, createInstallationResponse{
		InstallationToken:  installation.Token,
		InstallationID:     installation.ID,
		ChannelID:          channel.ID,
		ClaudeSessionToken: channel.ClaudeSessionToken,
		CreatedAt:          installation.CreatedAt,
	})
}

func (h *Handler) handleCreatePairing(w http.ResponseWriter, r *http.Request) {
	if h.pairings == nil || h.channels == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	auth, ok := h.requireScope(r, authScopeInstallation, authScopeAdmin)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	defer r.Body.Close()

	var payload createPairingRequest
	if r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}

	installationID := strings.TrimSpace(payload.InstallationID)
	switch auth.Scope {
	case authScopeInstallation:
		installationID = auth.InstallationID
	case authScopeAdmin:
		if installationID == "" {
			http.Error(w, "installationId is required for admin pairing", http.StatusBadRequest)
			return
		}
	}

	channel, err := h.channels.EnsureForInstallation(installationID)
	if err != nil {
		h.logger.Printf("api.pairing_channel_failed installation=%s error=%v", installationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	pairing, err := h.pairings.Create(installationID, h.pairingTTL)
	if err != nil {
		h.logger.Printf("api.pairing_create_failed installation=%s error=%v", installationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	pairingURL := h.pairingURL(pairing.Token)
	h.logger.Printf("api.pairing_created installation=%s pairing=%s channel=%s", installationID, pairing.ID, channel.ID)
	h.writeJSON(w, createPairingResponse{
		PairingID:    pairing.ID,
		PairingToken: pairing.Token,
		PairingURL:   pairingURL,
		TerminalQR:   buildTerminalQR(pairingURL),
		ExpiresAt:    pairing.ExpiresAt,
	})
}

func (h *Handler) handleGetPairing(w http.ResponseWriter, r *http.Request) {
	if h.pairings == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	auth, ok := h.requireScope(r, authScopeInstallation, authScopeAdmin)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	pairingID := strings.TrimSpace(r.PathValue("pairingID"))
	if pairingID == "" {
		http.NotFound(w, r)
		return
	}

	pairing, found := h.pairings.GetByID(pairingID)
	if !found {
		http.NotFound(w, r)
		return
	}

	if auth.Scope == authScopeInstallation && auth.InstallationID != pairing.InstallationID {
		http.NotFound(w, r)
		return
	}

	now := time.Now().UTC()
	status := pairing.StatusAt(now)
	response := pairingStatusResponse{
		Status: status,
	}

	switch status {
	case pairings.StatusPending:
		expiresAt := pairing.ExpiresAt
		response.ExpiresAt = &expiresAt
	case pairings.StatusPaired:
		pairedAt := pairing.ClaimedAt
		response.PairedAt = &pairedAt
		response.ChannelID = pairing.ChannelID
		if h.channels != nil {
			if channel, ok := h.channels.GetByID(pairing.ChannelID); ok {
				response.ClaudeSessionToken = channel.ClaudeSessionToken
			}
		}
	}

	h.writeJSON(w, response)
}

func (h *Handler) handlePairingQR(w http.ResponseWriter, r *http.Request) {
	pairingToken := normalizePairingToken(r.PathValue("pairingToken"))
	if pairingToken == "" {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	png, err := qrcode.Encode(h.pairingURL(pairingToken), qrcode.Medium, 256)
	if err != nil {
		h.logger.Printf("api.pairing_qr_encode_failed token=%s error=%v", pairingToken, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func (h *Handler) handleClaimPairing(w http.ResponseWriter, r *http.Request) {
	if h.pairings == nil || h.channels == nil || h.devices == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	if !h.allowRequestFromIP(r, "pairings:claim", h.claimPairingPerIP) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}

	defer r.Body.Close()

	var payload claimPairingRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	pairingToken := normalizePairingToken(payload.PairingToken)
	phoneInstallationID := strings.TrimSpace(payload.PhoneInstallationID)
	if pairingToken == "" || phoneInstallationID == "" || strings.TrimSpace(payload.PushToken) == "" {
		http.Error(w, "pairingToken, phoneInstallationId, and pushToken are required", http.StatusBadRequest)
		return
	}

	pairing, found := h.pairings.GetByToken(pairingToken)
	if !found {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	channel, err := h.channels.EnsureForInstallation(pairing.InstallationID)
	if err != nil {
		h.logger.Printf("api.pairing_claim_channel_failed installation=%s error=%v", pairing.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rotated, err := h.channels.RotatePhoneSession(pairing.InstallationID)
	if err != nil {
		h.logger.Printf("api.pairing_claim_rotate_failed installation=%s error=%v", pairing.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := h.devices.DeleteChannel(channel.ID); err != nil {
		h.logger.Printf("api.pairing_claim_device_cleanup_failed channel=%s error=%v", channel.ID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if _, err := h.devices.Upsert(devices.RegisterInput{
		InstallationID: phoneInstallationID,
		ChannelID:      channel.ID,
		Platform:       "ios",
		PushToken:      payload.PushToken,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	claimed, err := h.pairings.ClaimByToken(pairingToken, channel.ID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch {
		case errors.Is(err, pairings.ErrNotFound):
			statusCode = http.StatusNotFound
		case errors.Is(err, pairings.ErrExpired):
			statusCode = http.StatusGone
		case errors.Is(err, pairings.ErrAlreadyPaired):
			statusCode = http.StatusConflict
		}
		http.Error(w, http.StatusText(statusCode), statusCode)
		return
	}

	h.logger.Printf("api.pairing_claimed pairing=%s installation=%s channel=%s", claimed.ID, pairing.InstallationID, channel.ID)
	h.writeJSON(w, claimPairingResponse{
		PhoneSessionToken:  rotated.PhoneSessionToken,
		ChannelID:          channel.ID,
		PollIntervalSecond: 1,
	})
}

func (h *Handler) handleCreateWatchPairingCode(w http.ResponseWriter, r *http.Request) {
	if h.watchPairings == nil || h.channels == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	auth, ok := h.requireScope(r, authScopeInstallation, authScopeAdmin)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	defer r.Body.Close()

	var payload createWatchPairingCodeRequest
	if r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}

	installationID := strings.TrimSpace(payload.InstallationID)
	switch auth.Scope {
	case authScopeInstallation:
		installationID = auth.InstallationID
	case authScopeAdmin:
		if installationID == "" {
			http.Error(w, "installationId is required for admin pairing", http.StatusBadRequest)
			return
		}
	}

	channel, err := h.channels.EnsureForInstallation(installationID)
	if err != nil {
		h.logger.Printf("api.watch_pairing_code_channel_failed installation=%s error=%v", installationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	code, clearCode, err := h.watchPairings.Create(installationID, channel.ID, h.pairingTTL)
	if err != nil {
		h.logger.Printf("api.watch_pairing_code_create_failed installation=%s error=%v", installationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("api.watch_pairing_code_created installation=%s channel=%s code_id=%s", installationID, channel.ID, code.ID)
	h.writeJSON(w, createWatchPairingCodeResponse{
		Code:      clearCode,
		CodeID:    code.ID,
		ExpiresAt: code.ExpiresAt,
	})
}

func (h *Handler) handleClaimWatchPairingCode(w http.ResponseWriter, r *http.Request) {
	if h.watchPairings == nil || h.channels == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	if !h.allowRequestFromIP(r, "watch_pairings:claim", h.claimPairingPerIP) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}

	defer r.Body.Close()

	var payload claimWatchPairingRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	watchInstallationID := strings.TrimSpace(payload.WatchInstallationID)
	if watchInstallationID == "" {
		http.Error(w, "watchInstallationId is required", http.StatusBadRequest)
		return
	}

	claimed, err := h.watchPairings.Claim(payload.Code)
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch {
		case errors.Is(err, watchpairings.ErrInvalidCode):
			statusCode = http.StatusBadRequest
		case errors.Is(err, watchpairings.ErrNotFound):
			statusCode = http.StatusNotFound
		case errors.Is(err, watchpairings.ErrExpired):
			statusCode = http.StatusGone
		case errors.Is(err, watchpairings.ErrAlreadyClaimed):
			statusCode = http.StatusConflict
		case errors.Is(err, watchpairings.ErrTooManyAttempt):
			statusCode = http.StatusTooManyRequests
		}
		http.Error(w, http.StatusText(statusCode), statusCode)
		return
	}

	rotated, err := h.channels.RotateWatchSession(claimed.InstallationID)
	if err != nil {
		h.logger.Printf("api.watch_pairing_claim_rotate_failed installation=%s error=%v", claimed.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("api.watch_pairing_claimed code_id=%s installation=%s channel=%s", claimed.ID, claimed.InstallationID, rotated.ID)
	h.writeJSON(w, claimWatchPairingResponse{
		WatchSessionToken:  rotated.WatchSessionToken,
		ChannelID:          rotated.ID,
		PollIntervalSecond: defaultWatchPollInterval,
	})
}

func (h *Handler) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	auth, ok := h.requireScope(r, authScopeClaude, authScopeAdmin, authScopeLegacy)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	defer r.Body.Close()

	var payload createEventRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	eventType, parsed := events.ParseType(payload.Type)
	if !parsed {
		http.Error(w, "invalid event type", http.StatusBadRequest)
		return
	}

	channelID := strings.TrimSpace(payload.ChannelID)
	switch auth.Scope {
	case authScopeClaude:
		channelID = auth.ChannelID
	case authScopeLegacy:
		channelID = ""
	}

	event, err := h.store.AppendInput(events.AppendInput{
		ChannelID: channelID,
		Type:      eventType,
		Source:    payload.Source,
		Title:     payload.Title,
		Body:      payload.Body,
	})
	if err != nil {
		h.logger.Printf("api.event_persist_failed type=%s channel=%s error=%v", eventType, channelID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("api.event_created type=%s id=%d channel=%s source=%s", event.Type, event.ID, event.ChannelID, event.Source)

	h.writeJSON(w, createEventResponse{Event: event})
}

func (h *Handler) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	if h.devices == nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	auth, ok := h.requireScope(r, authScopePhone, authScopeWatch, authScopeAdmin, authScopeLegacy)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	defer r.Body.Close()

	var payload createDeviceRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	channelID := strings.TrimSpace(payload.ChannelID)
	switch auth.Scope {
	case authScopePhone:
		channelID = auth.ChannelID
	case authScopeWatch:
		channelID = auth.ChannelID
	case authScopeLegacy:
		channelID = ""
	}

	device, err := h.devices.Upsert(devices.RegisterInput{
		InstallationID: payload.InstallationID,
		ChannelID:      channelID,
		Platform:       payload.Platform,
		PushToken:      payload.PushToken,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.logger.Printf("api.device_registered installation=%s channel=%s platform=%s", device.InstallationID, device.ChannelID, device.Platform)
	h.writeJSON(w, createDeviceResponse{Device: device})
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	auth, ok := h.requireScope(r, authScopePhone, authScopeWatch, authScopeAdmin, authScopeLegacy)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	afterID, err := parseSince(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}

	channelID := strings.TrimSpace(r.URL.Query().Get("channelId"))
	switch auth.Scope {
	case authScopePhone:
		channelID = auth.ChannelID
	case authScopeWatch:
		channelID = auth.ChannelID
	case authScopeLegacy:
		channelID = ""
	}

	response := eventsResponse{
		Events: h.store.SinceForChannel(channelID, afterID),
	}
	h.logger.Printf("api.events_request since=%d channel=%s returned=%d", afterID, channelID, len(response.Events))
	h.writeJSON(w, response)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	auth, ok := h.requireScope(r, authScopePhone, authScopeWatch, authScopeAdmin, authScopeLegacy)
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	channelID := strings.TrimSpace(r.URL.Query().Get("channelId"))
	switch auth.Scope {
	case authScopePhone:
		channelID = auth.ChannelID
	case authScopeWatch:
		channelID = auth.ChannelID
	case authScopeLegacy:
		channelID = ""
	}

	latest, hasLatest := h.store.LatestForChannel(channelID)
	var current *events.Event
	if hasLatest {
		current = &latest
	}

	h.writeJSON(w, statusResponse{
		Current:        current,
		PushConfigured: h.pushEnabled,
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Printf("api.encode_failed error=%v", err)
	}
}

func parseSince(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return 0, nil
	}

	return strconv.ParseInt(raw, 10, 64)
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}

	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}

	return token, true
}

func normalizePairingToken(raw string) string {
	token := strings.TrimSpace(raw)
	if token == "" {
		return ""
	}

	if decoded, err := url.PathUnescape(token); err == nil && decoded != "" {
		token = decoded
	}
	if decoded, err := url.QueryUnescape(token); err == nil && decoded != "" {
		token = decoded
	}

	token = strings.Join(strings.Fields(token), "")
	return strings.TrimSpace(token)
}

func (h *Handler) pairingURL(token string) string {
	return h.pairBaseURL + "/p/" + strings.TrimSpace(token)
}

func buildTerminalQR(pairingURL string) string {
	trimmed := strings.TrimSpace(pairingURL)
	if trimmed == "" {
		return ""
	}

	code, err := qrcode.New(trimmed, qrcode.Low)
	if err != nil {
		return ""
	}
	code.DisableBorder = true

	bitmap := code.Bitmap()
	size := len(bitmap)
	if size == 0 {
		return ""
	}

	// Render with half-block characters so the QR is compact in Claude output.
	quietZoneModules := 1
	start := -quietZoneModules
	end := size + quietZoneModules

	pixelAt := func(x int, y int) bool {
		if y < 0 || y >= size || x < 0 || x >= len(bitmap[y]) {
			return false
		}
		return bitmap[y][x]
	}

	lines := make([]string, 0, (end-start+1)/2)
	for y := start; y < end; y += 2 {
		var line strings.Builder
		for x := start; x < end; x++ {
			top := pixelAt(x, y)
			bottom := pixelAt(x, y+1)
			switch {
			case top && bottom:
				line.WriteRune('█')
			case top:
				line.WriteRune('▀')
			case bottom:
				line.WriteRune('▄')
			default:
				line.WriteByte(' ')
			}
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}

	return strings.Join(lines, "\n")
}

func (h *Handler) allowRequestFromIP(r *http.Request, operation string, limit int) bool {
	if limit <= 0 {
		return true
	}

	ip := clientIP(r)
	if ip == "" {
		ip = "unknown"
	}

	key := operation + ":" + ip
	return h.limiter.Allow(key, limit, defaultRateLimitWindow)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return strings.TrimSpace(host)
}

type requestLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{
		hits: make(map[string][]time.Time),
	}
}

func (l *requestLimiter) Allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}
	if window <= 0 {
		window = time.Minute
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	cutoff := now.Add(-window)

	entries := l.hits[key]
	filtered := make([]time.Time, 0, len(entries)+1)
	for _, entry := range entries {
		if entry.After(cutoff) {
			filtered = append(filtered, entry)
		}
	}

	if len(filtered) >= limit {
		l.hits[key] = filtered
		return false
	}

	filtered = append(filtered, now)
	l.hits[key] = filtered
	return true
}
