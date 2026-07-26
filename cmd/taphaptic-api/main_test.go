package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPushSenderDisabledWithoutConfiguration(t *testing.T) {
	sender, err := loadPushSender(config{})
	if err != nil {
		t.Fatalf("loadPushSender returned an error: %v", err)
	}
	if sender != nil {
		t.Fatal("loadPushSender returned a sender without APNs configuration")
	}
}

func TestLoadPushSenderAcceptsInlinePrivateKey(t *testing.T) {
	sender, err := loadPushSender(config{
		apnsKeyID:      "KEY123",
		apnsTeamID:     "TEAM123",
		apnsTopic:      "com.example.watch",
		apnsPrivateKey: testPrivateKeyPEM(t),
	})
	if err != nil {
		t.Fatalf("loadPushSender returned an error: %v", err)
	}
	if sender == nil {
		t.Fatal("loadPushSender returned nil")
	}
}

func TestLoadPushSenderAcceptsPrivateKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path, []byte(testPrivateKeyPEM(t)), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	sender, err := loadPushSender(config{
		apnsKeyID:          "KEY123",
		apnsTeamID:         "TEAM123",
		apnsTopic:          "com.example.watch",
		apnsPrivateKeyPath: path,
	})
	if err != nil {
		t.Fatalf("loadPushSender returned an error: %v", err)
	}
	if sender == nil {
		t.Fatal("loadPushSender returned nil")
	}
}

func TestLoadPushSenderRejectsAmbiguousOrPartialConfiguration(t *testing.T) {
	privateKey := testPrivateKeyPEM(t)
	testCases := []config{
		{apnsKeyID: "KEY123"},
		{
			apnsKeyID:          "KEY123",
			apnsTeamID:         "TEAM123",
			apnsTopic:          "com.example.watch",
			apnsPrivateKeyPath: "/tmp/AuthKey.p8",
			apnsPrivateKey:     privateKey,
		},
	}

	for _, cfg := range testCases {
		if _, err := loadPushSender(cfg); err == nil {
			t.Fatal("loadPushSender accepted invalid APNs configuration")
		}
	}
}

func TestLoadConfigReadsInlinePrivateKey(t *testing.T) {
	privateKey := testPrivateKeyPEM(t)
	t.Setenv("TAPHAPTIC_APNS_PRIVATE_KEY", privateKey)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if cfg.apnsPrivateKey != strings.TrimSpace(privateKey) {
		t.Fatal("loadConfig did not preserve the inline private key")
	}
}

func testPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate private key: %v", err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
}
