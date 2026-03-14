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

	"agentwatch/internal/events"
	"agentwatch/internal/server"
)

const (
	defaultPort          = 7878
	defaultMaxEvents     = 32
	defaultServiceType   = "_agentwatch._tcp"
	defaultServiceDomain = "local"
	defaultDataDirName   = "AgentWatch"
	defaultStateFileName = "events.json"
)

type config struct {
	port        int
	token       string
	serviceName string
	statePath   string
}

type bonjourAdvertiser struct {
	cmd    *exec.Cmd
	exitCh chan error
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	if err := run(logger); err != nil {
		logger.Printf("server.exited_with_error error=%v", err)
		os.Exit(1)
	}
}

func run(logger *log.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config.load_failed: %w", err)
	}

	store, err := events.OpenStore(defaultMaxEvents, cfg.statePath)
	if err != nil {
		return fmt.Errorf("store.open_failed: %w", err)
	}

	handler := server.NewHandler(server.Config{
		Token:  cfg.token,
		Logger: logger,
		Store:  store,
	})

	listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", cfg.port))
	if err != nil {
		return fmt.Errorf("server.listen_failed: %w", err)
	}
	defer listener.Close()

	advertiser, err := startBonjour(cfg.serviceName, cfg.port)
	if err != nil {
		return fmt.Errorf("bonjour.start_failed: %w", err)
	}
	defer advertiser.Close()

	httpServer := &http.Server{
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- httpServer.Serve(listener)
	}()

	logger.Printf("server.started addr=%s service=%s state=%s", listener.Addr().String(), cfg.serviceName, cfg.statePath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Printf("server.shutting_down reason=signal")
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server.stopped_unexpectedly: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server.shutdown_failed: %w", err)
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

	token := strings.TrimSpace(os.Getenv("AGENTWATCH_TOKEN"))
	if token == "" {
		return config{}, errors.New("AGENTWATCH_TOKEN is required")
	}

	serviceName := strings.TrimSpace(os.Getenv("AGENTWATCH_SERVICE_NAME"))
	if serviceName == "" {
		host, err := os.Hostname()
		if err != nil || strings.TrimSpace(host) == "" {
			serviceName = "AgentWatch"
		} else {
			serviceName = host
		}
	}

	dataDir := strings.TrimSpace(os.Getenv("AGENTWATCH_DATA_DIR"))
	if dataDir == "" {
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			return config{}, fmt.Errorf("resolve user config dir: %w", err)
		}
		dataDir = filepath.Join(userConfigDir, defaultDataDirName)
	}

	return config{
		port:        port,
		token:       token,
		serviceName: serviceName,
		statePath:   filepath.Join(dataDir, defaultStateFileName),
	}, nil
}

func startBonjour(serviceName string, port int) (*bonjourAdvertiser, error) {
	path, err := exec.LookPath("dns-sd")
	if err != nil {
		return nil, fmt.Errorf("dns-sd not available: %w", err)
	}

	cmd := exec.Command(path, "-R", serviceName, defaultServiceType, defaultServiceDomain, strconv.Itoa(port))
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
