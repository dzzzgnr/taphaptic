package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"agentwatch/internal/devices"
	"agentwatch/internal/events"
)

type Notifier interface {
	NotifyEvent(ctx context.Context, event events.Event, devices []devices.Device) error
}

type NoopNotifier struct{}

func (NoopNotifier) NotifyEvent(context.Context, events.Event, []devices.Device) error {
	return nil
}

type Config struct {
	TeamID         string
	KeyID          string
	Topic          string
	PrivateKeyPath string
	UseSandbox     bool
	Logger         *log.Logger
	HTTPClient     *http.Client
}

type APNSNotifier struct {
	teamID     string
	keyID      string
	topic      string
	useSandbox bool
	logger     *log.Logger
	httpClient *http.Client
	privateKey *ecdsa.PrivateKey

	authMu         sync.Mutex
	cachedToken    string
	cachedIssuedAt time.Time
}

func NewAPNSNotifier(cfg Config) (*APNSNotifier, error) {
	if strings.TrimSpace(cfg.TeamID) == "" {
		return nil, errors.New("team ID is required")
	}
	if strings.TrimSpace(cfg.KeyID) == "" {
		return nil, errors.New("key ID is required")
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		return nil, errors.New("topic is required")
	}
	if strings.TrimSpace(cfg.PrivateKeyPath) == "" {
		return nil, errors.New("private key path is required")
	}

	privateKey, err := loadPrivateKey(cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	return &APNSNotifier{
		teamID:     strings.TrimSpace(cfg.TeamID),
		keyID:      strings.TrimSpace(cfg.KeyID),
		topic:      strings.TrimSpace(cfg.Topic),
		useSandbox: cfg.UseSandbox,
		logger:     logger,
		httpClient: httpClient,
		privateKey: privateKey,
	}, nil
}

func (n *APNSNotifier) NotifyEvent(ctx context.Context, event events.Event, devicesList []devices.Device) error {
	if len(devicesList) == 0 {
		return nil
	}

	bearerToken, err := n.bearerToken()
	if err != nil {
		return err
	}

	payload, err := json.Marshal(pushPayload{
		APS: apsPayload{
			Alert: apsAlert{
				Title: resolveEventTitle(event),
				Body:  resolveEventBody(event),
			},
			Sound:            "default",
			ContentAvailable: 1,
		},
		AgentWatch: agentWatchPayload{
			ID:        event.ID,
			Type:      string(event.Type),
			CreatedAt: event.CreatedAt.Format(time.RFC3339),
			Source:    event.Source,
			Title:     event.Title,
			Body:      event.Body,
		},
	})
	if err != nil {
		return fmt.Errorf("encode APNS payload: %w", err)
	}

	failures := 0
	for _, device := range devicesList {
		if device.Platform != "ios" && device.Platform != "watchos" {
			continue
		}

		if err := n.send(ctx, bearerToken, device.PushToken, payload); err != nil {
			failures++
			n.logger.Printf("api.push_failed installation=%s token=%s error=%v", device.InstallationID, device.PushToken, err)
		}
	}

	if failures > 0 {
		return fmt.Errorf("push failed for %d device(s)", failures)
	}

	return nil
}

func (n *APNSNotifier) send(ctx context.Context, bearerToken string, deviceToken string, payload []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL()+"/3/device/"+deviceToken, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+bearerToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("apns-push-type", "alert")
	request.Header.Set("apns-priority", "10")
	request.Header.Set("apns-topic", n.topic)

	response, err := n.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusOK {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, 256))
	if len(body) == 0 {
		return fmt.Errorf("APNS returned %s", response.Status)
	}

	return fmt.Errorf("APNS returned %s: %s", response.Status, strings.TrimSpace(string(body)))
}

func (n *APNSNotifier) baseURL() string {
	if n.useSandbox {
		return "https://api.sandbox.push.apple.com"
	}
	return "https://api.push.apple.com"
}

func (n *APNSNotifier) bearerToken() (string, error) {
	n.authMu.Lock()
	defer n.authMu.Unlock()

	now := time.Now().UTC()
	if n.cachedToken != "" && now.Sub(n.cachedIssuedAt) < 50*time.Minute {
		return n.cachedToken, nil
	}

	headerJSON, err := json.Marshal(map[string]string{
		"alg": "ES256",
		"kid": n.keyID,
	})
	if err != nil {
		return "", fmt.Errorf("encode JWT header: %w", err)
	}

	claimsJSON, err := json.Marshal(map[string]any{
		"iss": n.teamID,
		"iat": now.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode JWT claims: %w", err)
	}

	encodedHeader := encodeBase64URL(headerJSON)
	encodedClaims := encodeBase64URL(claimsJSON)
	unsignedToken := encodedHeader + "." + encodedClaims

	digest := sha256.Sum256([]byte(unsignedToken))
	signature, err := ecdsa.SignASN1(rand.Reader, n.privateKey, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	n.cachedIssuedAt = now
	n.cachedToken = unsignedToken + "." + encodeBase64URL(signature)
	return n.cachedToken, nil
}

func loadPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("decode private key PEM: no PEM block found")
	}

	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if privateKey, ok := key.(*ecdsa.PrivateKey); ok {
			return privateKey, nil
		}
		return nil, errors.New("decode private key: unsupported key type")
	}

	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}

	return privateKey, nil
}

func encodeBase64URL(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func resolveEventTitle(event events.Event) string {
	if title := strings.TrimSpace(event.Title); title != "" {
		return title
	}

	switch event.Type {
	case events.TypeCompleted:
		return "Completed"
	case events.TypeSubagentCompleted:
		return "Subagent Completed"
	case events.TypeFailed:
		return "Failed"
	case events.TypeAttention:
		return "Needs Attention"
	default:
		return "AgentWatch"
	}
}

func resolveEventBody(event events.Event) string {
	if body := strings.TrimSpace(event.Body); body != "" {
		return body
	}

	switch event.Type {
	case events.TypeCompleted:
		return "AGENT COMPLETED A TASK"
	case events.TypeSubagentCompleted:
		return "Claude Code subagent finished background work."
	case events.TypeFailed:
		return "Claude Code reported a failure."
	case events.TypeAttention:
		return "Claude Code needs your attention."
	default:
		return "A new AgentWatch event arrived."
	}
}

type pushPayload struct {
	APS        apsPayload        `json:"aps"`
	AgentWatch agentWatchPayload `json:"agentwatch"`
}

type apsPayload struct {
	Alert            apsAlert `json:"alert"`
	Sound            string   `json:"sound,omitempty"`
	ContentAvailable int      `json:"content-available,omitempty"`
}

type apsAlert struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type agentWatchPayload struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	CreatedAt string `json:"createdAt"`
	Source    string `json:"source,omitempty"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
}

var _ crypto.Signer = (*ecdsa.PrivateKey)(nil)
