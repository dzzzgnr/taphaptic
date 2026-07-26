package apns

import (
	"bytes"
	"context"
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
	"net/http"
	"strings"
	"sync"
	"time"

	"taphaptic/internal/devices"
	"taphaptic/internal/events"
)

const (
	productionBaseURL  = "https://api.push.apple.com"
	developmentBaseURL = "https://api.sandbox.push.apple.com"
)

type Config struct {
	KeyID      string
	TeamID     string
	PrivateKey *ecdsa.PrivateKey
	Client     *http.Client
	Now        func() time.Time
}

type Client struct {
	keyID      string
	teamID     string
	privateKey *ecdsa.PrivateKey
	httpClient *http.Client
	now        func() time.Time

	mu          sync.Mutex
	cachedJWT   string
	jwtIssuedAt time.Time
}

type ResponseError struct {
	StatusCode   int
	Reason       string
	Unregistered bool
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("APNs status=%d reason=%s", e.StatusCode, e.Reason)
}

func (e *ResponseError) DeviceTokenInvalid() bool {
	return e != nil && e.Unregistered
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.KeyID) == "" || strings.TrimSpace(cfg.TeamID) == "" || cfg.PrivateKey == nil {
		return nil, errors.New("APNs key ID, team ID, and private key are required")
	}
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		keyID:      strings.TrimSpace(cfg.KeyID),
		teamID:     strings.TrimSpace(cfg.TeamID),
		privateKey: cfg.PrivateKey,
		httpClient: httpClient,
		now:        now,
	}, nil
}

func ParsePrivateKey(raw []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("APNs private key is not PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse APNs PKCS8 key: %w", err)
	}
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("APNs private key is not ECDSA")
	}
	return ecdsaKey, nil
}

func (c *Client) Send(ctx context.Context, device devices.Device, event events.Event) error {
	token, err := c.authorizationToken()
	if err != nil {
		return err
	}
	payload := notificationPayload(event)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	baseURL := productionBaseURL
	if device.Environment == "development" {
		baseURL = developmentBaseURL
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/3/device/"+device.Token,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	request.Header.Set("authorization", "bearer "+token)
	request.Header.Set("apns-topic", device.Topic)
	request.Header.Set("apns-push-type", "alert")
	request.Header.Set("apns-priority", "10")
	request.Header.Set("apns-expiration", fmt.Sprintf("%d", c.now().Add(10*time.Minute).Unix()))
	request.Header.Set("content-type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}

	var failure struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&failure)
	if failure.Reason == "" {
		failure.Reason = http.StatusText(response.StatusCode)
	}
	return &ResponseError{
		StatusCode: response.StatusCode,
		Reason:     failure.Reason,
		Unregistered: response.StatusCode == http.StatusGone ||
			failure.Reason == "BadDeviceToken" ||
			failure.Reason == "DeviceTokenNotForTopic" ||
			failure.Reason == "Unregistered",
	}
}

func (c *Client) authorizationToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now().UTC()
	if c.cachedJWT != "" && now.Sub(c.jwtIssuedAt) < 50*time.Minute {
		return c.cachedJWT, nil
	}

	header, err := encodeJSONSegment(map[string]any{"alg": "ES256", "kid": c.keyID})
	if err != nil {
		return "", err
	}
	claims, err := encodeJSONSegment(map[string]any{"iss": c.teamID, "iat": now.Unix()})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + claims
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, c.privateKey, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	c.cachedJWT = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	c.jwtIssuedAt = now
	return c.cachedJWT, nil
}

func encodeJSONSegment(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func notificationPayload(event events.Event) map[string]any {
	title := strings.TrimSpace(event.Title)
	if title == "" {
		title = displaySource(event.Source) + " update"
	}
	body := strings.TrimSpace(event.Body)
	if body == "" {
		body = fallbackBody(event.Type)
	}
	return map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{
				"title": truncate(title, 80),
				"body":  truncate(body, 160),
			},
			"sound":     "default",
			"category":  "TAPHAPTIC_EVENT",
			"thread-id": "taphaptic-" + strings.TrimSpace(event.Source),
		},
		"eventId": event.ID,
		"type":    event.Type,
		"source":  event.Source,
	}
}

func displaySource(source string) string {
	switch strings.TrimSpace(source) {
	case "codex":
		return "Codex"
	case "claude-code":
		return "Claude Code"
	default:
		return "Agent"
	}
}

func fallbackBody(eventType events.Type) string {
	switch eventType {
	case events.TypeCompleted:
		return "Agent completed a task"
	case events.TypeSubagentCompleted:
		return "A subagent completed"
	case events.TypeFailed:
		return "Agent reported a failure"
	default:
		return "Agent needs your attention"
	}
}

func truncate(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes-1]) + "…"
}
