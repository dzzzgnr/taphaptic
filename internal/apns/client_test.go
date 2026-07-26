package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"taphaptic/internal/devices"
	"taphaptic/internal/events"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSendBuildsAlertRequest(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var gotRequest *http.Request
	client, err := NewClient(Config{
		KeyID:      "KEY123",
		TeamID:     "TEAM123",
		PrivateKey: key,
		Now:        func() time.Time { return time.Unix(1_800_000_000, 0) },
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			gotRequest = request
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Send(context.Background(), devices.Device{
		Token:       strings.Repeat("ab", 32),
		Environment: "development",
		Topic:       "com.example.taphaptic",
	}, events.Event{
		ID:     42,
		Type:   events.TypeCompleted,
		Source: "codex",
		Title:  "Codex completed",
		Body:   "Agent completed a task",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest == nil {
		t.Fatal("request was not sent")
	}
	if gotRequest.URL.Host != "api.sandbox.push.apple.com" {
		t.Fatalf("host=%q", gotRequest.URL.Host)
	}
	if gotRequest.Header.Get("apns-topic") != "com.example.taphaptic" {
		t.Fatalf("missing apns-topic")
	}
	if !strings.HasPrefix(gotRequest.Header.Get("authorization"), "bearer ") {
		t.Fatalf("missing authorization")
	}
}

func TestSendMarksUnregisteredToken(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	client, err := NewClient(Config{
		KeyID:      "KEY123",
		TeamID:     "TEAM123",
		PrivateKey: key,
		Client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusGone,
				Body:       io.NopCloser(strings.NewReader(`{"reason":"Unregistered"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Send(context.Background(), devices.Device{
		Token:       strings.Repeat("ab", 32),
		Environment: "production",
		Topic:       "com.example.taphaptic",
	}, events.Event{Type: events.TypeCompleted})
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || !responseErr.Unregistered {
		t.Fatalf("got %v, want unregistered ResponseError", err)
	}
}
