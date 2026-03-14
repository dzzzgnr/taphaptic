package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"agentwatch/internal/events"
)

type Config struct {
	Token  string
	Logger *log.Logger
	Store  *events.Store
}

type Handler struct {
	token  string
	logger *log.Logger
	store  *events.Store
}

type eventsResponse struct {
	Events []events.Event `json:"events"`
}

func NewHandler(cfg Config) *Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	return &Handler{
		token:  cfg.Token,
		logger: logger,
		store:  cfg.Store,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /complete", h.handleComplete)
	mux.HandleFunc("POST /failed", h.handleFailed)
	mux.HandleFunc("POST /attention", h.handleAttention)
	mux.HandleFunc("GET /events", h.handleEvents)
	return h.withAuth(mux)
}

func (h *Handler) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-AgentWatch-Token") != h.token {
			h.logger.Printf("auth.failed method=%s path=%s remote=%s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleComplete(w http.ResponseWriter, _ *http.Request) {
	event, err := h.store.Append(events.TypeCompleted)
	if err != nil {
		h.logger.Printf("event.persist_failed type=%s error=%v", events.TypeCompleted, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("event.created type=%s id=%d", event.Type, event.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleFailed(w http.ResponseWriter, _ *http.Request) {
	event, err := h.store.Append(events.TypeFailed)
	if err != nil {
		h.logger.Printf("event.persist_failed type=%s error=%v", events.TypeFailed, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("event.created type=%s id=%d", event.Type, event.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleAttention(w http.ResponseWriter, _ *http.Request) {
	event, err := h.store.Append(events.TypeAttention)
	if err != nil {
		h.logger.Printf("event.persist_failed type=%s error=%v", events.TypeAttention, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	h.logger.Printf("event.created type=%s id=%d", event.Type, event.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	afterID, err := parseSince(r)
	if err != nil {
		http.Error(w, "invalid since parameter", http.StatusBadRequest)
		return
	}

	response := eventsResponse{
		Events: h.store.Since(afterID),
	}
	h.logger.Printf("events.request since=%d returned=%d", afterID, len(response.Events))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Printf("events.encode_failed error=%v", err)
	}
}

func parseSince(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return 0, nil
	}

	return strconv.ParseInt(raw, 10, 64)
}
