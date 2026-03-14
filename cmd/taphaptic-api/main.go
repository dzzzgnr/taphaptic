package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"taphaptic/internal/api"
	"taphaptic/internal/channels"
	"taphaptic/internal/events"
	"taphaptic/internal/installations"
	"taphaptic/internal/watchpairings"
)

const (
	defaultHost                  = "0.0.0.0"
	defaultPort                  = 8080
	defaultMaxEvents             = 64
	defaultDataDirName           = "TaphapticAPI"
	defaultEventsFileName        = "events.json"
	defaultChannelsFileName      = "channels.json"
	defaultInstallationsFileName = "installations.json"
	defaultWatchCodesFileName    = "watch_pairings.json"
	defaultBonjourServiceType    = "_taphaptic._tcp"
	defaultBonjourDomain         = "local"
)

type config struct {
	host                   string
	port                   int
	serviceName            string
	eventsStatePath        string
	channelsStatePath      string
	installationsStatePath string
	watchCodesStatePath    string
}

type bonjourAdvertiser struct {
	cmd    *exec.Cmd
	exitCh chan error
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
		return fmt.Errorf("events.open_failed: %w", err)
	}

	channelStore, err := channels.OpenStore(cfg.channelsStatePath)
	if err != nil {
		return fmt.Errorf("channels.open_failed: %w", err)
	}

	installationStore, err := installations.OpenStore(cfg.installationsStatePath)
	if err != nil {
		return fmt.Errorf("installations.open_failed: %w", err)
	}

	watchCodeStore, err := watchpairings.OpenStore(cfg.watchCodesStatePath)
	if err != nil {
		return fmt.Errorf("watch_pairings.open_failed: %w", err)
	}

	handler := api.NewHandler(api.Config{
		Logger:            logger,
		Store:             store,
		ChannelStore:      channelStore,
		InstallationStore: installationStore,
		WatchPairingStore: watchCodeStore,
	})

	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	advertiser, advertiseErr := startBonjour(cfg.serviceName, cfg.port)
	if advertiseErr != nil {
		logger.Printf("api.bonjour_disabled error=%v", advertiseErr)
	} else {
		defer advertiser.Close()
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- httpServer.ListenAndServe()
	}()

	logger.Printf(
		"api.started addr=%s service=%s events=%s channels=%s installations=%s watch_pairings=%s",
		httpServer.Addr,
		cfg.serviceName,
		cfg.eventsStatePath,
		cfg.channelsStatePath,
		cfg.installationsStatePath,
		cfg.watchCodesStatePath,
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
	host := valueWithFallbacks(defaultHost, "TAPHAPTIC_BIND_HOST")

	port := defaultPort
	if rawPort := valueWithFallbacks("", "PORT", "TAPHAPTIC_PORT"); rawPort != "" {
		parsedPort, err := strconv.Atoi(rawPort)
		if err != nil || parsedPort <= 0 || parsedPort > 65535 {
			return config{}, fmt.Errorf("invalid port value %q", rawPort)
		}
		port = parsedPort
	}

	serviceName := strings.TrimSpace(valueWithFallbacks("", "TAPHAPTIC_SERVICE_NAME"))
	if serviceName == "" {
		hostname, err := os.Hostname()
		if err != nil || strings.TrimSpace(hostname) == "" {
			serviceName = "Taphaptic"
		} else {
			serviceName = hostname
		}
	}

	dataDir := strings.TrimSpace(valueWithFallbacks("", "TAPHAPTIC_DATA_DIR"))
	if dataDir == "" {
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			return config{}, fmt.Errorf("resolve user config dir: %w", err)
		}
		dataDir = filepath.Join(userConfigDir, defaultDataDirName)
	}

	return config{
		host:                   host,
		port:                   port,
		serviceName:            serviceName,
		eventsStatePath:        statePathFromEnv(filepath.Join(dataDir, defaultEventsFileName), "TAPHAPTIC_EVENTS_FILE"),
		channelsStatePath:      statePathFromEnv(filepath.Join(dataDir, defaultChannelsFileName), "TAPHAPTIC_CHANNELS_FILE"),
		installationsStatePath: statePathFromEnv(filepath.Join(dataDir, defaultInstallationsFileName), "TAPHAPTIC_INSTALLATIONS_FILE"),
		watchCodesStatePath:    statePathFromEnv(filepath.Join(dataDir, defaultWatchCodesFileName), "TAPHAPTIC_WATCH_PAIRINGS_FILE"),
	}, nil
}

func startBonjour(serviceName string, port int) (*bonjourAdvertiser, error) {
	path, err := exec.LookPath("dns-sd")
	if err != nil {
		return nil, fmt.Errorf("dns-sd not available: %w", err)
	}

	cmd := exec.Command(path, "-R", serviceName, defaultBonjourServiceType, defaultBonjourDomain, strconv.Itoa(port))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	advertiser := &bonjourAdvertiser{
		cmd:    cmd,
		exitCh: make(chan error, 1),
	}

	go func() {
		advertiser.exitCh <- cmd.Wait()
	}()

	select {
	case err := <-advertiser.exitCh:
		return nil, fmt.Errorf("dns-sd exited early: %w", err)
	case <-time.After(750 * time.Millisecond):
		return advertiser, nil
	}
}

func (a *bonjourAdvertiser) Close() {
	if a == nil || a.cmd == nil || a.cmd.Process == nil {
		return
	}

	_ = a.cmd.Process.Signal(os.Interrupt)

	select {
	case <-a.exitCh:
		return
	case <-time.After(2 * time.Second):
		_ = a.cmd.Process.Kill()
		<-a.exitCh
	}
}

func statePathFromEnv(fallback string, envNames ...string) string {
	for _, envName := range envNames {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value
		}
	}
	return fallback
}

func valueWithFallbacks(defaultValue string, envNames ...string) string {
	for _, envName := range envNames {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value
		}
	}
	return defaultValue
}
