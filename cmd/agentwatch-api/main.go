package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agentwatch/internal/api"
	"agentwatch/internal/channels"
	"agentwatch/internal/devices"
	"agentwatch/internal/events"
	"agentwatch/internal/installations"
	"agentwatch/internal/pairings"
	"agentwatch/internal/push"
	"agentwatch/internal/sessions"
	"agentwatch/internal/watchpairings"
)

const (
	defaultPort                  = 8080
	defaultMaxEvents             = 64
	defaultDataDirName           = "AgentWatchAPI"
	defaultEventsFileName        = "events.json"
	defaultDevicesFileName       = "devices.json"
	defaultSessionsFileName      = "sessions.json"
	defaultChannelsFileName      = "channels.json"
	defaultInstallationsFileName = "claude_installations.json"
	defaultPairingsFileName      = "pairings.json"
	defaultWatchCodesFileName    = "watch_pairings.json"
	defaultPairBaseURL           = "https://pairagentwatchapp.vercel.app"
	defaultPublicAPIBaseURL      = "https://agentwatch-api-production-39a1.up.railway.app"
)

type config struct {
	port                   int
	apiKey                 string
	loginSecret            string
	publicAPIBaseURL       string
	pairBaseURL            string
	eventsStatePath        string
	devicesStatePath       string
	sessionsStatePath      string
	channelsStatePath      string
	installationsStatePath string
	pairingsStatePath      string
	watchCodesStatePath    string
	pushConfig             pushConfig
}

type pushConfig struct {
	enabled        bool
	teamID         string
	keyID          string
	topic          string
	privateKeyPath string
	useSandbox     bool
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("api.exited_with_error error=%v", err)
		os.Exit(1)
	}
}

func run(logger *log.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config.load_failed: %w", err)
	}

	store, err := events.OpenStore(defaultMaxEvents, cfg.eventsStatePath)
	if err != nil {
		return fmt.Errorf("store.open_failed: %w", err)
	}

	deviceStore, err := devices.OpenStore(cfg.devicesStatePath)
	if err != nil {
		return fmt.Errorf("devices.open_failed: %w", err)
	}

	sessionStore, err := sessions.OpenStore(cfg.sessionsStatePath)
	if err != nil {
		return fmt.Errorf("sessions.open_failed: %w", err)
	}

	channelStore, err := channels.OpenStore(cfg.channelsStatePath)
	if err != nil {
		return fmt.Errorf("channels.open_failed: %w", err)
	}

	installationStore, err := installations.OpenStore(cfg.installationsStatePath)
	if err != nil {
		return fmt.Errorf("installations.open_failed: %w", err)
	}

	pairingStore, err := pairings.OpenStore(cfg.pairingsStatePath)
	if err != nil {
		return fmt.Errorf("pairings.open_failed: %w", err)
	}

	watchCodeStore, err := watchpairings.OpenStore(cfg.watchCodesStatePath)
	if err != nil {
		return fmt.Errorf("watch_pairings.open_failed: %w", err)
	}

	notifier, err := buildNotifier(cfg.pushConfig, logger)
	if err != nil {
		return fmt.Errorf("push.init_failed: %w", err)
	}

	handler := api.NewHandler(api.Config{
		APIKey:            cfg.apiKey,
		LoginSecret:       cfg.loginSecret,
		PublicAPIBaseURL:  cfg.publicAPIBaseURL,
		PairBaseURL:       cfg.pairBaseURL,
		Logger:            logger,
		Store:             store,
		DeviceStore:       deviceStore,
		Notifier:          notifier,
		PushEnabled:       cfg.pushConfig.enabled,
		SessionStore:      sessionStore,
		ChannelStore:      channelStore,
		InstallationStore: installationStore,
		PairingStore:      pairingStore,
		WatchPairingStore: watchCodeStore,
	})

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.port),
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- httpServer.ListenAndServe()
	}()

	logger.Printf(
		"api.started addr=%s events=%s devices=%s sessions=%s channels=%s installations=%s pairings=%s watch_pairings=%s push_enabled=%t",
		httpServer.Addr,
		cfg.eventsStatePath,
		cfg.devicesStatePath,
		cfg.sessionsStatePath,
		cfg.channelsStatePath,
		cfg.installationsStatePath,
		cfg.pairingsStatePath,
		cfg.watchCodesStatePath,
		cfg.pushConfig.enabled,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Printf("api.shutting_down reason=signal")
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("api.stopped_unexpectedly: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("api.shutdown_failed: %w", err)
	}

	return nil
}

func loadConfig() (config, error) {
	port := defaultPort
	if rawPort := os.Getenv("PORT"); rawPort != "" {
		parsedPort, err := strconv.Atoi(rawPort)
		if err != nil || parsedPort <= 0 || parsedPort > 65535 {
			return config{}, fmt.Errorf("invalid PORT value %q", rawPort)
		}
		port = parsedPort
	}

	apiKey := strings.TrimSpace(os.Getenv("AGENTWATCH_API_KEY"))
	if apiKey == "" {
		return config{}, errors.New("AGENTWATCH_API_KEY is required")
	}

	loginSecret := strings.TrimSpace(os.Getenv("AGENTWATCH_LOGIN_SECRET"))
	if loginSecret == "" {
		loginSecret = apiKey
	}

	publicAPIBaseURL := strings.TrimSpace(os.Getenv("AGENTWATCH_PUBLIC_API_BASE_URL"))
	if publicAPIBaseURL == "" {
		publicAPIBaseURL = defaultPublicAPIBaseURL
	}

	pairBaseURL := strings.TrimSpace(os.Getenv("AGENTWATCH_PAIR_BASE_URL"))
	if pairBaseURL == "" {
		pairBaseURL = defaultPairBaseURL
	}

	dataDir := strings.TrimSpace(os.Getenv("AGENTWATCH_DATA_DIR"))
	if dataDir == "" {
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			return config{}, fmt.Errorf("resolve user config dir: %w", err)
		}
		dataDir = filepath.Join(userConfigDir, defaultDataDirName)
	}

	pushCfg, err := loadPushConfig()
	if err != nil {
		return config{}, err
	}

	return config{
		port:                   port,
		apiKey:                 apiKey,
		loginSecret:            loginSecret,
		publicAPIBaseURL:       publicAPIBaseURL,
		pairBaseURL:            pairBaseURL,
		pushConfig:             pushCfg,
		eventsStatePath:        statePathFromEnv("AGENTWATCH_EVENTS_FILE", filepath.Join(dataDir, defaultEventsFileName)),
		devicesStatePath:       statePathFromEnv("AGENTWATCH_DEVICES_FILE", filepath.Join(dataDir, defaultDevicesFileName)),
		sessionsStatePath:      statePathFromEnv("AGENTWATCH_SESSIONS_FILE", filepath.Join(dataDir, defaultSessionsFileName)),
		channelsStatePath:      statePathFromEnv("AGENTWATCH_CHANNELS_FILE", filepath.Join(dataDir, defaultChannelsFileName)),
		installationsStatePath: statePathFromEnv("AGENTWATCH_INSTALLATIONS_FILE", filepath.Join(dataDir, defaultInstallationsFileName)),
		pairingsStatePath:      statePathFromEnv("AGENTWATCH_PAIRINGS_FILE", filepath.Join(dataDir, defaultPairingsFileName)),
		watchCodesStatePath:    statePathFromEnv("AGENTWATCH_WATCH_PAIRINGS_FILE", filepath.Join(dataDir, defaultWatchCodesFileName)),
	}, nil
}

func buildNotifier(cfg pushConfig, logger *log.Logger) (push.Notifier, error) {
	if !cfg.enabled {
		logger.Printf("api.push_disabled reason=missing_apns_config")
		return push.NoopNotifier{}, nil
	}

	return push.NewAPNSNotifier(push.Config{
		TeamID:         cfg.teamID,
		KeyID:          cfg.keyID,
		Topic:          cfg.topic,
		PrivateKeyPath: cfg.privateKeyPath,
		UseSandbox:     cfg.useSandbox,
		Logger:         logger,
	})
}

func statePathFromEnv(envName string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}

	return fallback
}

func loadPushConfig() (pushConfig, error) {
	cfg := pushConfig{
		useSandbox: true,
	}

	cfg.teamID = strings.TrimSpace(os.Getenv("AGENTWATCH_APNS_TEAM_ID"))
	cfg.keyID = strings.TrimSpace(os.Getenv("AGENTWATCH_APNS_KEY_ID"))
	cfg.topic = strings.TrimSpace(os.Getenv("AGENTWATCH_APNS_TOPIC"))
	cfg.privateKeyPath = strings.TrimSpace(os.Getenv("AGENTWATCH_APNS_PRIVATE_KEY_PATH"))

	provided := 0
	for _, value := range []string{cfg.teamID, cfg.keyID, cfg.topic, cfg.privateKeyPath} {
		if value != "" {
			provided++
		}
	}

	if provided == 0 {
		return cfg, nil
	}
	if provided != 4 {
		return pushConfig{}, errors.New("APNS config requires AGENTWATCH_APNS_TEAM_ID, AGENTWATCH_APNS_KEY_ID, AGENTWATCH_APNS_TOPIC, and AGENTWATCH_APNS_PRIVATE_KEY_PATH")
	}

	cfg.enabled = true

	if raw := strings.TrimSpace(os.Getenv("AGENTWATCH_APNS_SANDBOX")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return pushConfig{}, fmt.Errorf("invalid AGENTWATCH_APNS_SANDBOX value %q", raw)
		}
		cfg.useSandbox = parsed
	}

	return cfg, nil
}
