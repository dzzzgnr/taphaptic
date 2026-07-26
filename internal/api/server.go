package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"taphaptic/internal/channels"
	"taphaptic/internal/devices"
	"taphaptic/internal/events"
	"taphaptic/internal/installations"
	"taphaptic/internal/watchpairings"
)

const (
	defaultPairingTTL         = 10 * time.Minute
	defaultRateLimitWindow    = time.Minute
	defaultCreateInstallPerIP = 12
	defaultClaimPairingPerIP  = 30
	defaultEventsPerMinute    = 120
	defaultTestPushPerMinute  = 5
	defaultDevicesPerMinute   = 20
	defaultWatchPollInterval  = 1
)

type Config struct {
	Logger             *log.Logger
	Store              *events.Store
	ChannelStore       *channels.Store
	InstallationStore  *installations.Store
	WatchPairingStore  *watchpairings.Store
	DeviceStore        *devices.Store
	PushSender         PushSender
	APNSTopic          string
	APNSEnvironment    string
	TrustProxyHeaders  bool
	PairingTTL         time.Duration
	CreateInstallPerIP int
	ClaimPairingPerIP  int
}

type Handler struct {
	logger             *log.Logger
	store              *events.Store
	channels           *channels.Store
	installations      *installations.Store
	watchPairings      *watchpairings.Store
	devices            *devices.Store
	pushSender         PushSender
	apnsTopic          string
	apnsEnvironment    string
	trustProxyHeaders  bool
	pairingTTL         time.Duration
	createInstallPerIP int
	claimPairingPerIP  int
	limiter            *requestLimiter
}

type PushSender interface {
	Send(context.Context, devices.Device, events.Event) error
}

type invalidDeviceTokenError interface {
	DeviceTokenInvalid() bool
}

type createEventRequest struct {
	SchemaVersion int       `json:"schemaVersion,omitempty"`
	Type          string    `json:"type"`
	OccurredAt    time.Time `json:"occurredAt,omitempty"`
	Source        string    `json:"source,omitempty"`
	SourceEvent   string    `json:"sourceEvent,omitempty"`
	DedupeKey     string    `json:"dedupeKey,omitempty"`
	Title         string    `json:"title,omitempty"`
	Body          string    `json:"body,omitempty"`
}

type createEventResponse struct {
	Event        events.Event `json:"event"`
	Deduplicated bool         `json:"deduplicated,omitempty"`
}

type createInstallationResponse struct {
	InstallationToken  string    `json:"installationToken"`
	InstallationID     string    `json:"installationId"`
	ChannelID          string    `json:"channelId,omitempty"`
	ProducerToken      string    `json:"producerToken,omitempty"`
	ClaudeSessionToken string    `json:"claudeSessionToken,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
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

type registerWatchDeviceRequest struct {
	WatchInstallationID string `json:"watchInstallationId"`
	PushToken           string `json:"pushToken"`
}

type testNotificationResponse struct {
	Sent int `json:"sent"`
}

type eventsResponse struct {
	Events []events.Event `json:"events"`
}

type authScope string

const (
	authScopeUnknown      authScope = "unknown"
	authScopeInstallation authScope = "installation"
	authScopeProducer     authScope = "producer"
	authScopeWatch        authScope = "watch"
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

	deviceStore := cfg.DeviceStore
	if deviceStore == nil {
		deviceStore = devices.NewStore()
	}
	apnsEnvironment := strings.ToLower(strings.TrimSpace(cfg.APNSEnvironment))
	if apnsEnvironment == "" {
		apnsEnvironment = "production"
	}

	return &Handler{
		logger:             logger,
		store:              cfg.Store,
		channels:           cfg.ChannelStore,
		installations:      cfg.InstallationStore,
		watchPairings:      cfg.WatchPairingStore,
		devices:            deviceStore,
		pushSender:         cfg.PushSender,
		apnsTopic:          strings.TrimSpace(cfg.APNSTopic),
		apnsEnvironment:    apnsEnvironment,
		trustProxyHeaders:  cfg.TrustProxyHeaders,
		pairingTTL:         pairingTTL,
		createInstallPerIP: createInstallPerIP,
		claimPairingPerIP:  claimPairingPerIP,
		limiter:            newRequestLimiter(),
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("POST /v1/installations", h.handleCreateInstallation)
	mux.HandleFunc("POST /v1/claude/installations", h.handleCreateInstallation)
	mux.HandleFunc("DELETE /v1/installations/current", h.handleDeleteInstallation)
	mux.HandleFunc("POST /v1/watch/pairings/code", h.handleCreateWatchPairingCode)
	mux.HandleFunc("POST /v1/watch/pairings/claim", h.handleClaimWatchPairingCode)
	mux.HandleFunc("POST /v1/watch/devices", h.handleRegisterWatchDevice)
	mux.HandleFunc("DELETE /v1/watch/devices", h.handleDeleteWatchDevice)
	mux.HandleFunc("POST /v1/watch/test-notification", h.handleTestNotification)
	mux.HandleFunc("POST /v1/events", h.handleCreateEvent)
	mux.HandleFunc("GET /v1/events", h.handleEvents)
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
	switch path {
	case "/healthz", "/v1/installations", "/v1/claude/installations", "/v1/watch/pairings/claim":
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

	if installation, found := h.installations.GetByToken(token); found {
		_ = h.installations.TouchToken(token)
		return authContext{
			Scope:          authScopeInstallation,
			Token:          token,
			InstallationID: installation.ID,
		}
	}

	if channel, found := h.channels.GetByProducerToken(token); found {
		return authContext{
			Scope:          authScopeProducer,
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

	return authContext{Scope: authScopeUnknown}
}

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateInstallation(w http.ResponseWriter, r *http.Request) {
	auth := h.resolveAuth(r.Header.Get("Authorization"))
	if auth.Scope != authScopeInstallation {
		ip := h.clientIP(r)
		if !h.limiter.Allow("install:"+ip, h.createInstallPerIP, defaultRateLimitWindow) {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}
	}

	var installation installations.Installation
	var err error
	if auth.Scope == authScopeInstallation {
		installation, _ = h.installations.GetByToken(auth.Token)
	} else {
		installation, err = h.installations.Create()
		if err != nil {
			h.logger.Printf("api.installation_create_failed error=%v", err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	channel, err := h.channels.EnsureForInstallation(installation.ID)
	if err != nil {
		h.logger.Printf("api.channel_create_failed installation=%s error=%v", installation.ID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, createInstallationResponse{
		InstallationToken:  installation.Token,
		InstallationID:     installation.ID,
		ChannelID:          channel.ID,
		ProducerToken:      channel.ProducerToken,
		ClaudeSessionToken: channel.ProducerToken,
		CreatedAt:          installation.CreatedAt,
	})
}

func (h *Handler) handleDeleteInstallation(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeInstallation {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	channel, hasChannel := h.channels.GetByInstallation(auth.InstallationID)
	if err := h.watchPairings.RemoveForInstallation(auth.InstallationID); err != nil {
		h.logger.Printf("api.installation_delete_pairings_failed installation=%s error=%v", auth.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := h.devices.RemoveForInstallation(auth.InstallationID); err != nil {
		h.logger.Printf("api.installation_delete_devices_failed installation=%s error=%v", auth.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if hasChannel {
		if err := h.store.RemoveForChannel(channel.ID); err != nil {
			h.logger.Printf("api.installation_delete_events_failed installation=%s error=%v", auth.InstallationID, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
	if err := h.channels.RemoveByInstallation(auth.InstallationID); err != nil {
		h.logger.Printf("api.installation_delete_channel_failed installation=%s error=%v", auth.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := h.installations.RemoveByID(auth.InstallationID); err != nil {
		h.logger.Printf("api.installation_delete_identity_failed installation=%s error=%v", auth.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateWatchPairingCode(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeInstallation {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	var payload createWatchPairingCodeRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if payload.InstallationID != "" && strings.TrimSpace(payload.InstallationID) != auth.InstallationID {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	channel, err := h.channels.EnsureForInstallation(auth.InstallationID)
	if err != nil {
		h.logger.Printf("api.watch_pairing_channel_failed installation=%s error=%v", auth.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	code, clearCode, err := h.watchPairings.Create(auth.InstallationID, channel.ID, h.pairingTTL)
	if err != nil {
		h.logger.Printf("api.watch_pairing_create_failed installation=%s error=%v", auth.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, createWatchPairingCodeResponse{
		Code:      clearCode,
		CodeID:    code.ID,
		ExpiresAt: code.ExpiresAt,
	})
}

func (h *Handler) handleClaimWatchPairingCode(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	if !h.limiter.Allow("claim:"+ip, h.claimPairingPerIP, defaultRateLimitWindow) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}

	var payload claimWatchPairingRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(payload.WatchInstallationID) == "" {
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

	channel, err := h.channels.RotateWatchSession(claimed.InstallationID)
	if err != nil {
		h.logger.Printf("api.watch_session_rotate_failed installation=%s error=%v", claimed.InstallationID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(payload.PushToken) != "" && h.apnsTopic != "" {
		if _, deviceErr := h.devices.Upsert(devices.Device{
			InstallationID:      claimed.InstallationID,
			WatchInstallationID: payload.WatchInstallationID,
			Token:               payload.PushToken,
			Environment:         h.apnsEnvironment,
			Topic:               h.apnsTopic,
		}); deviceErr != nil {
			h.logger.Printf("api.watch_device_registration_ignored installation=%s error=%v", claimed.InstallationID, deviceErr)
		}
	}

	h.writeJSON(w, claimWatchPairingResponse{
		WatchSessionToken:  channel.WatchSessionToken,
		ChannelID:          channel.ID,
		PollIntervalSecond: defaultWatchPollInterval,
	})
}

func (h *Handler) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeProducer {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if !h.limiter.Allow("event:"+auth.InstallationID, defaultEventsPerMinute, defaultRateLimitWindow) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}

	var payload createEventRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	eventType, ok := events.ParseType(payload.Type)
	if !ok {
		http.Error(w, "invalid event type", http.StatusBadRequest)
		return
	}

	created, appended, err := h.store.AppendUnique(events.AppendInput{
		SchemaVersion: payload.SchemaVersion,
		ChannelID:     auth.ChannelID,
		Type:          eventType,
		OccurredAt:    payload.OccurredAt,
		Source:        payload.Source,
		SourceEvent:   payload.SourceEvent,
		DedupeKey:     payload.DedupeKey,
		Title:         payload.Title,
		Body:          payload.Body,
	})
	if err != nil {
		h.logger.Printf("api.event_create_failed channel=%s error=%v", auth.ChannelID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, createEventResponse{
		Event:        created,
		Deduplicated: !appended,
	})
	if appended {
		h.deliverEvent(r.Context(), auth.InstallationID, created)
	}
}

func (h *Handler) handleRegisterWatchDevice(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeWatch {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if !h.limiter.Allow("device:"+auth.InstallationID, defaultDevicesPerMinute, defaultRateLimitWindow) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}
	if h.apnsTopic == "" {
		http.Error(w, "push notifications are not configured", http.StatusServiceUnavailable)
		return
	}
	var payload registerWatchDeviceRequest
	if err := decodeJSONBody(r, &payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	device, err := h.devices.Upsert(devices.Device{
		InstallationID:      auth.InstallationID,
		WatchInstallationID: payload.WatchInstallationID,
		Token:               payload.PushToken,
		Environment:         h.apnsEnvironment,
		Topic:               h.apnsTopic,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.writeJSON(w, map[string]any{
		"watchInstallationId": device.WatchInstallationID,
		"registered":          true,
	})
}

func (h *Handler) handleDeleteWatchDevice(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeWatch {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	watchInstallationID := strings.TrimSpace(r.URL.Query().Get("watchInstallationId"))
	if watchInstallationID == "" {
		http.Error(w, "watchInstallationId is required", http.StatusBadRequest)
		return
	}
	err := h.devices.Remove(auth.InstallationID, watchInstallationID)
	if errors.Is(err, devices.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeWatch {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	if !h.limiter.Allow("test-push:"+auth.InstallationID, defaultTestPushPerMinute, defaultRateLimitWindow) {
		http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		return
	}
	event := events.Event{
		ID:            time.Now().UTC().UnixMilli(),
		SchemaVersion: 1,
		Type:          events.TypeCompleted,
		CreatedAt:     time.Now().UTC(),
		OccurredAt:    time.Now().UTC(),
		Source:        "taphaptic",
		SourceEvent:   "TestNotification",
		Title:         "Taphaptic is connected",
		Body:          "Test notification delivered",
	}
	sent := h.deliverEvent(r.Context(), auth.InstallationID, event)
	h.writeJSON(w, testNotificationResponse{Sent: sent})
}

func (h *Handler) deliverEvent(ctx context.Context, installationID string, event events.Event) int {
	if h.pushSender == nil {
		return 0
	}
	sent := 0
	for _, device := range h.devices.ForInstallation(installationID) {
		if err := h.pushSender.Send(ctx, device, event); err != nil {
			var tokenErr invalidDeviceTokenError
			if errors.As(err, &tokenErr) && tokenErr.DeviceTokenInvalid() {
				h.devices.RemoveByToken(device.Token)
			}
			h.logger.Printf(
				"api.push_failed installation=%s watch=%s source=%s type=%s error=%v",
				installationID,
				device.WatchInstallationID,
				event.Source,
				event.Type,
				err,
			)
			continue
		}
		sent++
	}
	return sent
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	auth, ok := authFromContext(r.Context())
	if !ok || auth.Scope != authScopeWatch {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	afterID, err := parseSince(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}

	response := eventsResponse{
		Events: h.store.SinceForChannel(auth.ChannelID, afterID),
	}

	h.writeJSON(w, response)
}

func (h *Handler) writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		h.logger.Printf("api.json_encode_failed error=%v", err)
	}
}

func decodeJSONBody(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func authFromContext(ctx context.Context) (authContext, bool) {
	value := ctx.Value(authContextKey{})
	auth, ok := value.(authContext)
	if !ok {
		return authContext{}, false
	}
	return auth, true
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

func parseSince(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func (h *Handler) clientIP(r *http.Request) string {
	if h.trustProxyHeaders {
		forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if len(parts) > 0 {
				first := strings.TrimSpace(parts[0])
				if first != "" {
					return first
				}
			}
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

type requestBucket struct {
	windowStart time.Time
	count       int
}

type requestLimiter struct {
	mu      sync.Mutex
	buckets map[string]requestBucket
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{buckets: make(map[string]requestBucket)}
}

func (l *requestLimiter) Allow(key string, maxCount int, window time.Duration) bool {
	if maxCount <= 0 || key == "" {
		return true
	}
	if window <= 0 {
		window = defaultRateLimitWindow
	}

	now := time.Now().UTC()

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, found := l.buckets[key]
	if !found || now.Sub(bucket.windowStart) >= window {
		l.buckets[key] = requestBucket{windowStart: now, count: 1}
		return true
	}

	if bucket.count >= maxCount {
		return false
	}

	bucket.count++
	l.buckets[key] = bucket
	return true
}
