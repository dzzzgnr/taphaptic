package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL = "http://127.0.0.1:8080"
)

type eventPayload struct {
	SchemaVersion int       `json:"schemaVersion,omitempty"`
	Type          string    `json:"type"`
	OccurredAt    time.Time `json:"occurredAt,omitempty"`
	Source        string    `json:"source,omitempty"`
	SourceEvent   string    `json:"sourceEvent,omitempty"`
	DedupeKey     string    `json:"dedupeKey,omitempty"`
	Title         string    `json:"title,omitempty"`
	Body          string    `json:"body,omitempty"`
}

type installationResponse struct {
	InstallationToken  string `json:"installationToken"`
	InstallationID     string `json:"installationId"`
	ProducerToken      string `json:"producerToken"`
	ClaudeSessionToken string `json:"claudeSessionToken"`
}

func (r installationResponse) EventProducerToken() string {
	if token := strings.TrimSpace(r.ProducerToken); token != "" {
		return token
	}
	return strings.TrimSpace(r.ClaudeSessionToken)
}

type pairingCodeResponse struct {
	Code string `json:"code"`
}

type claimPairingResponse struct {
	WatchSessionToken string `json:"watchSessionToken"`
}

type eventsResponse struct {
	Events []eventPayload `json:"events"`
}

type appPaths struct {
	HomeDir               string
	InstallRoot           string
	InstallBinDir         string
	InstalledHookPath     string
	InstalledCtlPath      string
	ClaudeRoot            string
	CodexRoot             string
	APIBaseURLFile        string
	InstallationTokenFile string
	InstallationIDFile    string
	ProducerTokenFile     string
	LegacyClaudeTokenFile string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: taphapticctl <install-consumer|delete-installation|patch-settings|patch-codex-hooks|remove-hooks|emit|health|smoke-local-e2e>")
	}

	switch args[0] {
	case "install-consumer":
		return runInstallConsumer(args[1:])
	case "delete-installation":
		return runDeleteInstallation(args[1:])
	case "patch-settings":
		return runPatchSettings(args[1:])
	case "patch-codex-hooks":
		return runPatchCodexHooks(args[1:])
	case "remove-hooks":
		return runRemoveHooks(args[1:])
	case "emit":
		return runEmit(args[1:])
	case "health":
		return runHealth(args[1:])
	case "smoke-local-e2e":
		return runSmokeLocalE2E(args[1:])
	default:
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func runDeleteInstallation(args []string) error {
	fs := flag.NewFlagSet("delete-installation", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiBaseURL := fs.String("api-base-url", "", "API base URL")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl delete-installation [--api-base-url <url>]")
	}
	paths, err := resolveAppPaths()
	if err != nil {
		return err
	}
	baseURLRaw := strings.TrimSpace(*apiBaseURL)
	if baseURLRaw == "" {
		baseURLRaw = readTrimmedFile(paths.APIBaseURLFile)
	}
	baseURL, err := normalizedBaseURL(baseURLRaw)
	if err != nil {
		return err
	}
	installationToken := readTrimmedFile(paths.InstallationTokenFile)
	if installationToken == "" {
		return nil
	}
	client := &http.Client{Timeout: 5 * time.Second}
	status, body, err := deleteRequest(
		client,
		joinURL(baseURL, "/v1/installations/current"),
		installationToken,
	)
	if err != nil {
		return fmt.Errorf("delete installation: %w", err)
	}
	if status != http.StatusNoContent && status != http.StatusUnauthorized {
		return fmt.Errorf("delete installation: status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	for _, path := range []string{
		paths.InstallationTokenFile,
		paths.InstallationIDFile,
		paths.ProducerTokenFile,
		paths.LegacyClaudeTokenFile,
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func runInstallConsumer(args []string) error {
	fs := flag.NewFlagSet("install-consumer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiBaseURL := fs.String("api-base-url", envOrDefault("TAPHAPTIC_API_BASE_URL", defaultAPIBaseURL), "API base URL")
	providersRaw := fs.String("provider", "all", "event provider: claude|codex|all")
	scope := fs.String("scope", "user", "settings scope: user|project")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl install-consumer [--api-base-url <url>] [--provider claude|codex|all] [--scope user|project]")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl install-consumer [--api-base-url <url>] [--provider claude|codex|all] [--scope user|project]")
	}

	if os.Getenv("SUDO_COMMAND") != "" || os.Geteuid() == 0 {
		return fmt.Errorf("do not run this installer with sudo/root")
	}

	paths, err := resolveAppPaths()
	if err != nil {
		return err
	}

	baseURL, err := normalizedBaseURL(*apiBaseURL)
	if err != nil {
		return err
	}
	providers, err := parseProviders(*providersRaw)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	if _, err := resolveProviderConfigPath("claude", *scope, cwd, paths.HomeDir); err != nil {
		return err
	}

	if err := ensureDir(paths.InstallBinDir, 0o700); err != nil {
		return err
	}

	if err := installCtlBinary(paths.InstalledCtlPath); err != nil {
		return err
	}

	if err := installHookWrapper(paths.InstalledHookPath, paths.InstalledCtlPath); err != nil {
		return err
	}

	if err := writeFileAtomic(paths.APIBaseURLFile, []byte(baseURL), 0o600); err != nil {
		return err
	}

	if providers.Claude {
		settingsPath, pathErr := resolveProviderConfigPath("claude", *scope, cwd, paths.HomeDir)
		if pathErr != nil {
			return pathErr
		}
		if err := backupFileIfExists(settingsPath); err != nil {
			return err
		}
		if err := patchSettingsAtPath(settingsPath, true); err != nil {
			return err
		}
	}
	if providers.Codex {
		hooksPath, pathErr := resolveProviderConfigPath("codex", *scope, cwd, paths.HomeDir)
		if pathErr != nil {
			return pathErr
		}
		if err := backupFileIfExists(hooksPath); err != nil {
			return err
		}
		if err := patchCodexHooksAtPath(hooksPath); err != nil {
			return err
		}
	}

	client := &http.Client{Timeout: 4 * time.Second}
	existingInstallToken := readTrimmedFile(paths.InstallationTokenFile)
	installResp, err := createOrRestoreInstallation(client, baseURL, existingInstallToken)
	if err != nil {
		return fmt.Errorf("failed to create or restore local installation identity. Is the Taphaptic API running at %s?", baseURL)
	}

	if err := writeFileAtomic(paths.InstallationTokenFile, []byte(installResp.InstallationToken), 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(paths.InstallationIDFile, []byte(installResp.InstallationID), 0o600); err != nil {
		return err
	}
	producerToken := installResp.EventProducerToken()
	if producerToken == "" {
		return errors.New("installation response is missing producerToken")
	}
	if err := writeFileAtomic(paths.ProducerTokenFile, []byte(producerToken), 0o600); err != nil {
		return err
	}

	code, err := createPairingCode(client, baseURL, installResp.InstallationToken)
	if err != nil {
		return fmt.Errorf("failed to create watch pairing code")
	}

	fmt.Println()
	printPairingCode(code)
	if providers.Codex {
		fmt.Println("Codex hooks were installed. Review and trust the Taphaptic hooks in Codex before use.")
	}
	fmt.Println()
	return nil
}

func runPatchSettings(args []string) error {
	fs := flag.NewFlagSet("patch-settings", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scope := fs.String("scope", "user", "settings scope: user|project")
	withNotifications := fs.Bool("with-notifications", false, "add notification hooks")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl patch-settings [--scope user|project] [--with-notifications]")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl patch-settings [--scope user|project] [--with-notifications]")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	settingsPath, err := resolveSettingsPath(*scope, cwd, homeDir)
	if err != nil {
		return err
	}
	if err := patchSettingsAtPath(settingsPath, *withNotifications); err != nil {
		return err
	}

	fmt.Printf("Updated Claude settings at %s\n", settingsPath)
	return nil
}

type providerSelection struct {
	Claude bool
	Codex  bool
}

func parseProviders(raw string) (providerSelection, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "claude", "claude-code":
		return providerSelection{Claude: true}, nil
	case "codex":
		return providerSelection{Codex: true}, nil
	case "all", "both":
		return providerSelection{Claude: true, Codex: true}, nil
	default:
		return providerSelection{}, fmt.Errorf("unsupported provider %q (use claude, codex, or all)", raw)
	}
}

func runPatchCodexHooks(args []string) error {
	fs := flag.NewFlagSet("patch-codex-hooks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scope := fs.String("scope", "user", "settings scope: user|project")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl patch-codex-hooks [--scope user|project]")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	hooksPath, err := resolveProviderConfigPath("codex", *scope, cwd, homeDir)
	if err != nil {
		return err
	}
	if err := backupFileIfExists(hooksPath); err != nil {
		return err
	}
	if err := patchCodexHooksAtPath(hooksPath); err != nil {
		return err
	}

	fmt.Printf("Updated Codex hooks at %s\n", hooksPath)
	fmt.Println("Review and trust the Taphaptic hooks in Codex before use.")
	return nil
}

func runRemoveHooks(args []string) error {
	fs := flag.NewFlagSet("remove-hooks", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	providersRaw := fs.String("provider", "all", "event provider: claude|codex|all")
	scope := fs.String("scope", "user", "settings scope: user|project")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl remove-hooks [--provider claude|codex|all] [--scope user|project]")
	}

	providers, err := parseProviders(*providersRaw)
	if err != nil {
		return err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	if providers.Claude {
		path, pathErr := resolveProviderConfigPath("claude", *scope, cwd, homeDir)
		if pathErr != nil {
			return pathErr
		}
		if err := removeTaphapticHooksAtPath(path); err != nil {
			return err
		}
	}
	if providers.Codex {
		path, pathErr := resolveProviderConfigPath("codex", *scope, cwd, homeDir)
		if pathErr != nil {
			return pathErr
		}
		if err := removeTaphapticHooksAtPath(path); err != nil {
			return err
		}
	}
	return nil
}

func runEmit(args []string) error {
	fs := flag.NewFlagSet("emit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	action := fs.String("action", "", "hook action")
	source := fs.String("source", "claude-code", "event source: claude-code|codex")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl emit --source <claude-code|codex> --action <stop|subagent_stop|permission_request|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention>")
	}
	if fs.NArg() != 0 || strings.TrimSpace(*action) == "" {
		return fmt.Errorf("usage: taphapticctl emit --source <claude-code|codex> --action <stop|subagent_stop|permission_request|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention>")
	}

	hookFields := readHookFields(os.Stdin)
	payload, err := eventForSourceAction(*source, *action, hookFields)
	if err != nil {
		return err
	}

	paths, err := resolveAppPaths()
	if err != nil {
		return err
	}

	apiBaseURL := strings.TrimSpace(os.Getenv("TAPHAPTIC_API_BASE_URL"))
	if apiBaseURL == "" {
		apiBaseURL = readTrimmedFile(paths.APIBaseURLFile)
	}
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}

	baseURL, err := normalizedBaseURL(apiBaseURL)
	if err != nil {
		return nil
	}

	producerToken := strings.TrimSpace(os.Getenv("TAPHAPTIC_PRODUCER_TOKEN"))
	if producerToken == "" {
		producerToken = strings.TrimSpace(os.Getenv("TAPHAPTIC_CLAUDE_SESSION_TOKEN"))
	}
	if producerToken == "" {
		producerToken = readTrimmedFile(paths.ProducerTokenFile)
	}
	if producerToken == "" {
		producerToken = readTrimmedFile(paths.LegacyClaudeTokenFile)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	if producerToken == "" {
		installationToken := readTrimmedFile(paths.InstallationTokenFile)
		if installationToken != "" {
			if installResp, resolveErr := createInstallation(client, baseURL, installationToken); resolveErr == nil {
				producerToken = installResp.EventProducerToken()
				if producerToken != "" {
					_ = writeFileAtomic(paths.ProducerTokenFile, []byte(producerToken), 0o600)
				}
			}
		}
	}

	if producerToken == "" {
		return nil
	}

	_ = postEvent(client, baseURL, producerToken, payload)
	return nil
}

func runHealth(args []string) error {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	baseURL := fs.String("base-url", defaultAPIBaseURL, "API base URL")
	timeoutMS := fs.Int("timeout-ms", 1000, "request timeout milliseconds")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl health [--base-url <url>] [--timeout-ms <ms>]")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl health [--base-url <url>] [--timeout-ms <ms>]")
	}

	normalizedURL, err := normalizedBaseURL(*baseURL)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Duration(*timeoutMS) * time.Millisecond}
	status, _, err := getRequest(client, joinURL(normalizedURL, "/healthz"), "")
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return fmt.Errorf("unhealthy status: %d", status)
	}
	return nil
}

func runSmokeLocalE2E(args []string) error {
	fs := flag.NewFlagSet("smoke-local-e2e", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiBaseURL := fs.String("api-base-url", "http://127.0.0.1:18080", "API base URL")
	port := fs.Int("port", 18080, "API port for spawned smoke server")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl smoke-local-e2e [--api-base-url <url>] [--port <n>]")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl smoke-local-e2e [--api-base-url <url>] [--port <n>]")
	}

	baseURL, err := normalizedBaseURL(*apiBaseURL)
	if err != nil {
		return err
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	apiBin := filepath.Join(repoRoot, "bin", "taphaptic-api")
	info, err := os.Stat(apiBin)
	if err != nil || info.Mode()&0o111 == 0 {
		return fmt.Errorf("API binary not found at %s. Build it first.", apiBin)
	}

	tmpDir, err := os.MkdirTemp("", "taphaptic-smoke-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	logFilePath := filepath.Join(tmpDir, "api.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open smoke log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.Command(apiBin)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		"TAPHAPTIC_BIND_HOST=127.0.0.1",
		"TAPHAPTIC_PORT="+strconv.Itoa(*port),
		"TAPHAPTIC_DATA_DIR="+filepath.Join(tmpDir, "data"),
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start API: %w", err)
	}

	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	if err := waitForHealth(client, baseURL, 5*time.Second); err != nil {
		logTail := tailFile(logFilePath, 40)
		return fmt.Errorf("API failed health check.\n%s", logTail)
	}

	if err := smokeAgainstBaseURL(client, baseURL); err != nil {
		return err
	}

	if cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() {
			_, _ = cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		cmd.Process = nil
	}

	fmt.Println("Smoke E2E passed (installation -> pairing -> claim -> event -> poll).")
	return nil
}

func smokeAgainstBaseURL(client *http.Client, baseURL string) error {
	installResp, err := createInstallation(client, baseURL, "")
	if err != nil {
		return fmt.Errorf("invalid installation response: %w", err)
	}

	code, err := createPairingCode(client, baseURL, installResp.InstallationToken)
	if err != nil {
		return fmt.Errorf("invalid pairing response: %w", err)
	}

	claimResp, err := claimPairingCode(client, baseURL, code, "watch-smoke")
	if err != nil {
		return fmt.Errorf("invalid claim response: %w", err)
	}

	if err := postEvent(client, baseURL, installResp.ClaudeSessionToken, eventPayload{
		Type:   "completed",
		Source: "smoke",
		Title:  "smoke",
		Body:   "smoke",
	}); err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	events, err := fetchEvents(client, baseURL, claimResp.WatchSessionToken, 0)
	if err != nil {
		return fmt.Errorf("invalid events response: %w", err)
	}
	if len(events.Events) < 1 || events.Events[0].Type != "completed" {
		return fmt.Errorf("invalid events response: %+v", events)
	}

	return nil
}

func createOrRestoreInstallation(client *http.Client, baseURL, existingToken string) (installationResponse, error) {
	if strings.TrimSpace(existingToken) != "" {
		restored, err := createInstallation(client, baseURL, existingToken)
		if err == nil {
			return restored, nil
		}
	}
	return createInstallation(client, baseURL, "")
}

func createInstallation(client *http.Client, baseURL, bearer string) (installationResponse, error) {
	resp, status, body, err := createInstallationAt(client, joinURL(baseURL, "/v1/installations"), bearer)
	if err != nil {
		return installationResponse{}, err
	}
	if status == http.StatusNotFound {
		resp, status, body, err = createInstallationAt(client, joinURL(baseURL, "/v1/claude/installations"), bearer)
		if err != nil {
			return installationResponse{}, err
		}
	}
	if status != http.StatusOK {
		return installationResponse{}, fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	if strings.TrimSpace(resp.InstallationToken) == "" || strings.TrimSpace(resp.InstallationID) == "" || resp.EventProducerToken() == "" {
		return installationResponse{}, errors.New("missing installation fields")
	}
	return resp, nil
}

func createInstallationAt(client *http.Client, requestURL, bearer string) (installationResponse, int, []byte, error) {
	var resp installationResponse
	status, body, err := postJSON(client, requestURL, bearer, map[string]any{}, &resp)
	if err != nil {
		return installationResponse{}, status, body, err
	}
	return resp, status, body, nil
}

func createPairingCode(client *http.Client, baseURL, installationToken string) (string, error) {
	var resp pairingCodeResponse
	status, body, err := postJSON(client, joinURL(baseURL, "/v1/watch/pairings/code"), installationToken, map[string]any{}, &resp)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	code := strings.TrimSpace(resp.Code)
	if code == "" {
		return "", errors.New("missing code")
	}
	return code, nil
}

func claimPairingCode(client *http.Client, baseURL, code, watchInstallationID string) (claimPairingResponse, error) {
	var resp claimPairingResponse
	payload := map[string]any{
		"code":                code,
		"watchInstallationId": watchInstallationID,
	}
	status, body, err := postJSON(client, joinURL(baseURL, "/v1/watch/pairings/claim"), "", payload, &resp)
	if err != nil {
		return claimPairingResponse{}, err
	}
	if status != http.StatusOK {
		return claimPairingResponse{}, fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	if strings.TrimSpace(resp.WatchSessionToken) == "" {
		return claimPairingResponse{}, errors.New("missing watchSessionToken")
	}
	return resp, nil
}

func postEvent(client *http.Client, baseURL, claudeToken string, payload eventPayload) error {
	status, body, err := postJSON(client, joinURL(baseURL, "/v1/events"), claudeToken, payload, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	return nil
}

func fetchEvents(client *http.Client, baseURL, watchToken string, since int64) (eventsResponse, error) {
	eventsURL := joinURL(baseURL, "/v1/events")
	values := url.Values{}
	values.Set("since", strconv.FormatInt(since, 10))
	eventsURL += "?" + values.Encode()

	var resp eventsResponse
	status, body, err := getJSON(client, eventsURL, watchToken, &resp)
	if err != nil {
		return eventsResponse{}, err
	}
	if status != http.StatusOK {
		return eventsResponse{}, fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func waitForHealth(client *http.Client, baseURL string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		status, _, err := getRequest(client, joinURL(baseURL, "/healthz"), "")
		if err == nil && status == http.StatusNoContent {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for healthz")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func postJSON(client *http.Client, requestURL, bearer string, payload any, out any) (int, []byte, error) {
	body := []byte("{}")
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = raw
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}

	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, err
	}

	if out != nil && res.StatusCode >= 200 && res.StatusCode < 300 && len(bytes.TrimSpace(resBody)) > 0 {
		if err := json.Unmarshal(resBody, out); err != nil {
			return res.StatusCode, resBody, err
		}
	}

	return res.StatusCode, resBody, nil
}

func getJSON(client *http.Client, requestURL, bearer string, out any) (int, []byte, error) {
	status, body, err := getRequest(client, requestURL, bearer)
	if err != nil {
		return status, body, err
	}
	if out != nil && status >= 200 && status < 300 && len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return status, body, err
		}
	}
	return status, body, nil
}

func getRequest(client *http.Client, requestURL, bearer string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}

	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, resBody, nil
}

func deleteRequest(client *http.Client, requestURL, bearer string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, err
	}
	return res.StatusCode, resBody, nil
}

func resolveAppPaths() (appPaths, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return appPaths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	installRoot := filepath.Join(homeDir, "Library", "Application Support", "Taphaptic")
	installBinDir := filepath.Join(installRoot, "bin")

	return appPaths{
		HomeDir:               homeDir,
		InstallRoot:           installRoot,
		InstallBinDir:         installBinDir,
		InstalledHookPath:     filepath.Join(installBinDir, "taphaptic-hook"),
		InstalledCtlPath:      filepath.Join(installBinDir, "taphapticctl"),
		ClaudeRoot:            filepath.Join(homeDir, ".claude"),
		CodexRoot:             filepath.Join(homeDir, ".codex"),
		APIBaseURLFile:        filepath.Join(installRoot, "api-base-url"),
		InstallationTokenFile: filepath.Join(installRoot, "installation-token"),
		InstallationIDFile:    filepath.Join(installRoot, "installation-id"),
		ProducerTokenFile:     filepath.Join(installRoot, "producer-token"),
		LegacyClaudeTokenFile: filepath.Join(installRoot, "claude-session-token"),
	}, nil
}

func installCtlBinary(destination string) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if err := copyFile(src, destination, 0o755); err != nil {
		return err
	}
	if err := ensureSignaturePreserved(src, destination); err != nil {
		return err
	}
	return nil
}

func ensureSignaturePreserved(src, dst string) error {
	if !commandAvailable("codesign") {
		return nil
	}

	signed, err := hasCodeSignature(src)
	if err != nil {
		return err
	}
	if !signed {
		return nil
	}

	if err := verifyCodeSignature(dst); err != nil {
		return fmt.Errorf("installed binary signature check failed: %w", err)
	}
	return nil
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func hasCodeSignature(path string) (bool, error) {
	cmd := exec.Command("codesign", "-dv", path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok && exitErr != nil {
		message := strings.ToLower(string(output))
		if strings.Contains(message, "code object is not signed") || strings.Contains(message, "not signed at all") {
			return false, nil
		}
		return false, fmt.Errorf("inspect code signature for %s: %s", path, strings.TrimSpace(string(output)))
	}

	return false, fmt.Errorf("inspect code signature for %s: %w", path, err)
}

func verifyCodeSignature(path string) error {
	cmd := exec.Command("codesign", "--verify", "--strict", "--verbose=2", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign verify for %s failed: %s", path, strings.TrimSpace(string(output)))
	}
	return nil
}

func installHookWrapper(destination, ctlBinaryPath string) error {
	hookScript := "#!/bin/sh\n\nset -eu\n\nsource=\"${1:-claude-code}\"\naction=\"${2:-}\"\nexec " + strconv.Quote(ctlBinaryPath) + " emit --source \"$source\" --action \"$action\"\n"
	if err := writeFileAtomic(destination, []byte(hookScript), 0o755); err != nil {
		return err
	}
	return nil
}

func resolveSettingsPath(scope, cwd, homeDir string) (string, error) {
	return resolveProviderConfigPath("claude", scope, cwd, homeDir)
}

func resolveProviderConfigPath(provider, scope, cwd, homeDir string) (string, error) {
	var directoryName, fileName string
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "claude-code":
		directoryName = ".claude"
		fileName = "settings.json"
	case "codex":
		directoryName = ".codex"
		fileName = "hooks.json"
	default:
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}

	switch strings.TrimSpace(scope) {
	case "user":
		return filepath.Join(homeDir, directoryName, fileName), nil
	case "project":
		return filepath.Join(cwd, directoryName, fileName), nil
	default:
		return "", fmt.Errorf("unsupported scope: %s (use user or project)", scope)
	}
}

func patchSettingsAtPath(settingsPath string, withNotifications bool) error {
	if err := ensureDir(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	config := map[string]any{}
	if raw, err := os.ReadFile(settingsPath); err == nil {
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return fmt.Errorf("invalid JSON in %s: %w", settingsPath, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := mergeClaudeHooks(config, withNotifications); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return writeFileAtomic(settingsPath, encoded, 0o600)
}

func patchCodexHooksAtPath(hooksPath string) error {
	if err := ensureDir(filepath.Dir(hooksPath), 0o755); err != nil {
		return err
	}

	config := map[string]any{}
	if raw, err := os.ReadFile(hooksPath); err == nil {
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &config); err != nil {
				return fmt.Errorf("invalid JSON in %s: %w", hooksPath, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := mergeCodexHooks(config); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(hooksPath, append(encoded, '\n'), 0o600)
}

func mergeCodexHooks(config map[string]any) error {
	hooksConfig := map[string]any{}
	if existingHooks, ok := config["hooks"]; ok && existingHooks != nil {
		casted, ok := existingHooks.(map[string]any)
		if !ok {
			return errors.New("Codex hooks key 'hooks' must be an object")
		}
		hooksConfig = casted
	}

	commands := []struct {
		event   string
		matcher string
		action  string
	}{
		{event: "Stop", matcher: "*", action: "stop"},
		{event: "SubagentStop", matcher: "*", action: "subagent_stop"},
		{event: "PermissionRequest", matcher: "*", action: "permission_request"},
	}
	for _, item := range commands {
		entries, err := ensureHookEntries(hooksConfig, item.event)
		if err != nil {
			return fmt.Errorf("Codex %w", err)
		}
		command := `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" codex ` + item.action
		hooksConfig[item.event] = addCommand(entries, item.matcher, command)
	}

	if _, ok := config["description"]; !ok {
		config["description"] = "Local lifecycle hooks, including Taphaptic Apple Watch notifications."
	}
	config["hooks"] = hooksConfig
	return nil
}

func removeTaphapticHooksAtPath(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	config := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	hooksAny, ok := config["hooks"]
	if !ok {
		return nil
	}
	hooksConfig, ok := hooksAny.(map[string]any)
	if !ok {
		return fmt.Errorf("hooks key in %s must be an object", path)
	}

	for event, entriesAny := range hooksConfig {
		entries, ok := entriesAny.([]any)
		if !ok {
			continue
		}
		cleaned := removeTaphapticEntries(entries)
		if len(cleaned) == 0 {
			delete(hooksConfig, event)
		} else {
			hooksConfig[event] = cleaned
		}
	}
	if len(hooksConfig) == 0 {
		delete(config, "hooks")
	} else {
		config["hooks"] = hooksConfig
	}

	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(encoded, '\n'), 0o600)
}

func removeTaphapticEntries(entries []any) []any {
	filtered := make([]any, 0, len(entries))
	for _, entryAny := range entries {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			filtered = append(filtered, entryAny)
			continue
		}
		hooksAny, ok := entry["hooks"]
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		handlers, ok := hooksAny.([]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}
		cleanedHandlers := make([]any, 0, len(handlers))
		for _, handlerAny := range handlers {
			handler, ok := handlerAny.(map[string]any)
			if !ok {
				cleanedHandlers = append(cleanedHandlers, handlerAny)
				continue
			}
			command, _ := handler["command"].(string)
			if strings.Contains(strings.ToLower(command), "taphaptic-hook") {
				continue
			}
			cleanedHandlers = append(cleanedHandlers, handler)
		}
		if len(cleanedHandlers) == 0 {
			continue
		}
		copied := cloneMap(entry)
		copied["hooks"] = cleanedHandlers
		filtered = append(filtered, copied)
	}
	return filtered
}

func mergeClaudeHooks(config map[string]any, withNotifications bool) error {
	hooksConfig := map[string]any{}
	if existingHooks, ok := config["hooks"]; ok && existingHooks != nil {
		casted, ok := existingHooks.(map[string]any)
		if !ok {
			return errors.New("Claude settings key 'hooks' must be an object")
		}
		hooksConfig = casted
	}

	stopEntries, err := ensureHookEntries(hooksConfig, "Stop")
	if err != nil {
		return err
	}
	stopEntries = pruneLegacyEntries(stopEntries)
	stopEntries = addCommand(stopEntries, "*", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" claude-code stop`)
	hooksConfig["Stop"] = stopEntries

	subagentEntries, err := ensureHookEntries(hooksConfig, "SubagentStop")
	if err != nil {
		return err
	}
	subagentEntries = pruneLegacyEntries(subagentEntries)
	subagentEntries = addCommand(subagentEntries, "*", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" claude-code subagent_stop`)
	hooksConfig["SubagentStop"] = subagentEntries

	if withNotifications {
		notificationEntries, err := ensureHookEntries(hooksConfig, "Notification")
		if err != nil {
			return err
		}
		notificationEntries = pruneLegacyEntries(notificationEntries)
		notificationEntries = addCommand(notificationEntries, "permission_prompt", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" claude-code permission_prompt`)
		notificationEntries = addCommand(notificationEntries, "idle_prompt", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" claude-code idle_prompt`)
		hooksConfig["Notification"] = notificationEntries
	}

	config["hooks"] = hooksConfig
	delete(config, "Stop")
	delete(config, "SubagentStop")
	delete(config, "Notification")
	return nil
}

func ensureHookEntries(hooksConfig map[string]any, key string) ([]any, error) {
	value, ok := hooksConfig[key]
	if !ok || value == nil {
		return []any{}, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("Claude settings key '%s' must be an array", key)
	}
	return entries, nil
}

func pruneLegacyEntries(entries []any) []any {
	filtered := make([]any, 0, len(entries))
	for _, entryAny := range entries {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			continue
		}

		hooksAny, hasHooks := entry["hooks"]
		if !hasHooks {
			filtered = append(filtered, entry)
			continue
		}
		hooks, ok := hooksAny.([]any)
		if !ok {
			filtered = append(filtered, entry)
			continue
		}

		cleaned := make([]any, 0, len(hooks))
		for _, hookAny := range hooks {
			hook, ok := hookAny.(map[string]any)
			if !ok {
				continue
			}
			command, _ := hook["command"].(string)
			if shouldDropLegacyHook(command) {
				continue
			}
			cleaned = append(cleaned, hook)
		}

		if len(cleaned) == 0 {
			continue
		}
		copied := cloneMap(entry)
		copied["hooks"] = cleaned
		filtered = append(filtered, copied)
	}
	return filtered
}

func shouldDropLegacyHook(command string) bool {
	normalized := strings.ToLower(command)
	if strings.Contains(normalized, "taphaptic-hook") {
		return true
	}
	return strings.Contains(normalized, "/library/application support/") &&
		strings.Contains(normalized, "/bin/") &&
		strings.Contains(normalized, "watch-hook")
}

func hasCommand(entries []any, command string) bool {
	for _, entryAny := range entries {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			continue
		}
		hooksAny, ok := entry["hooks"]
		if !ok {
			continue
		}
		hooks, ok := hooksAny.([]any)
		if !ok {
			continue
		}
		for _, hookAny := range hooks {
			hook, ok := hookAny.(map[string]any)
			if !ok {
				continue
			}
			hookType, _ := hook["type"].(string)
			hookCommand, _ := hook["command"].(string)
			if hookType == "command" && hookCommand == command {
				return true
			}
		}
	}
	return false
}

func addCommand(entries []any, matcher, command string) []any {
	if hasCommand(entries, command) {
		return entries
	}
	return append(entries, map[string]any{
		"matcher": matcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
			},
		},
	})
}

func eventForAction(action string) (eventPayload, error) {
	return eventForSourceAction("claude-code", action, nil)
}

func eventForSourceAction(source, action string, hookFields map[string]any) (eventPayload, error) {
	source, err := normalizedEventSource(source)
	if err != nil {
		return eventPayload{}, err
	}
	normalizedAction := strings.ToLower(strings.TrimSpace(action))
	payload := eventPayload{
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
		Source:        source,
		SourceEvent:   sourceEventName(normalizedAction),
	}

	switch normalizedAction {
	case "stop", "completed":
		payload.Type = "completed"
		payload.Title = sourceDisplayName(source) + " completed"
		payload.Body = "Agent completed a task"
	case "subagent_stop", "subagent_completed":
		payload.Type = "subagent_completed"
		payload.Title = sourceDisplayName(source) + " subagent completed"
		payload.Body = "An agent finished background work"
	case "failed":
		payload.Type = "failed"
		payload.Title = sourceDisplayName(source) + " failed"
		payload.Body = "Agent reported a failure"
	case "permission_request", "permission_prompt":
		payload.Type = "attention"
		payload.Title = sourceDisplayName(source) + " needs permission"
		payload.Body = "Agent is waiting for permission"
	case "idle_prompt":
		payload.Type = "attention"
		payload.Title = sourceDisplayName(source) + " is waiting"
		payload.Body = "Agent is idle and waiting for input"
	case "attention":
		payload.Type = "attention"
		payload.Title = sourceDisplayName(source) + " needs attention"
		payload.Body = "Agent needs attention"
	default:
		return eventPayload{}, fmt.Errorf("usage: taphapticctl emit --source <claude-code|codex> --action <stop|subagent_stop|permission_request|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention>")
	}
	payload.DedupeKey = eventDedupeKey(payload.Source, payload.SourceEvent, hookFields)
	return payload, nil
}

func normalizedEventSource(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "claude", "claude-code":
		return "claude-code", nil
	case "codex":
		return "codex", nil
	default:
		return "", fmt.Errorf("unsupported event source %q (use claude-code or codex)", raw)
	}
}

func sourceDisplayName(source string) string {
	if source == "codex" {
		return "Codex"
	}
	return "Claude Code"
}

func sourceEventName(action string) string {
	switch action {
	case "stop", "completed":
		return "Stop"
	case "subagent_stop", "subagent_completed":
		return "SubagentStop"
	case "permission_request":
		return "PermissionRequest"
	case "permission_prompt", "idle_prompt":
		return "Notification"
	case "failed":
		return "Failed"
	case "attention":
		return "Attention"
	default:
		return action
	}
}

func eventDedupeKey(source, eventName string, fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	sessionID := stringField(fields, "session_id")
	turnID := stringField(fields, "turn_id")
	subagentID := stringField(fields, "agent_id")
	if sessionID == "" && turnID == "" && subagentID == "" {
		return ""
	}
	return strings.Join([]string{source, sessionID, turnID, subagentID, eventName}, ":")
}

func stringField(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return strings.TrimSpace(value)
}

func readHookFields(stdin *os.File) map[string]any {
	if stdin == nil {
		return nil
	}
	info, err := stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 1024*1024))
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

func printPairingCode(code string) {
	lines := formatPairingCodeDisplay(code, terminalSupportsANSI(os.Stdout))
	for _, line := range lines {
		fmt.Println(line)
	}
}

func formatPairingCodeDisplay(code string, useANSI bool) []string {
	normalized := strings.TrimSpace(code)
	if normalized == "" {
		return nil
	}

	// Spacing between digits makes the short code easier to parse quickly.
	spaced := spaceSeparatedCode(normalized)
	lines := []string{
		"Enter this 4-digit pairing code on your Apple Watch:",
	}

	if useANSI {
		lines = append(lines, fmt.Sprintf("\x1b[1;97;44m  %s  \x1b[0m", spaced))
		return lines
	}

	border := strings.Repeat("=", len(spaced)+4)
	lines = append(lines,
		border,
		fmt.Sprintf("| %s |", spaced),
		border,
	)
	return lines
}

func terminalSupportsANSI(stdout *os.File) bool {
	if stdout == nil {
		return false
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if term == "" || term == "dumb" {
		return false
	}
	info, err := stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func spaceSeparatedCode(code string) string {
	var builder strings.Builder
	first := true
	for _, r := range code {
		if !first {
			builder.WriteByte(' ')
		}
		builder.WriteRune(r)
		first = false
	}
	return builder.String()
}

func normalizedBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = defaultAPIBaseURL
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid base URL %q", raw)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", raw)
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func envOrDefault(envName, fallback string) string {
	value := strings.TrimSpace(os.Getenv(envName))
	if value == "" {
		return fallback
	}
	return value
}

func readTrimmedFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func ensureDir(path string, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := ensureDir(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp." + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := ensureDir(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func backupFileIfExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}

	backupPath := path + ".backup." + time.Now().UTC().Format("20060102T150405Z")
	if err := copyFile(path, backupPath, 0o600); err != nil {
		return err
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func tailFile(path string, lines int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	parts := strings.Split(string(raw), "\n")
	if len(parts) <= lines {
		return string(raw)
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}
