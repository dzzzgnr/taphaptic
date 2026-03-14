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
	Type   string `json:"type"`
	Source string `json:"source,omitempty"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
}

type installationResponse struct {
	InstallationToken string `json:"installationToken"`
	InstallationID    string `json:"installationId"`
	ClaudeSessionToken string `json:"claudeSessionToken"`
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
	HomeDir              string
	InstallRoot          string
	InstallBinDir        string
	InstalledHookPath    string
	InstalledCtlPath     string
	ClaudeRoot           string
	APIBaseURLFile       string
	InstallationTokenFile string
	InstallationIDFile   string
	ClaudeTokenFile      string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: taphapticctl <install-consumer|patch-settings|emit|health|smoke-local-e2e>")
	}

	switch args[0] {
	case "install-consumer":
		return runInstallConsumer(args[1:])
	case "patch-settings":
		return runPatchSettings(args[1:])
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

func runInstallConsumer(args []string) error {
	fs := flag.NewFlagSet("install-consumer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	apiBaseURL := fs.String("api-base-url", envOrDefault("TAPHAPTIC_API_BASE_URL", defaultAPIBaseURL), "API base URL")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl install-consumer [--api-base-url <url>]")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: taphapticctl install-consumer [--api-base-url <url>]")
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

	settingsPath := filepath.Join(paths.ClaudeRoot, "settings.json")
	if err := backupFileIfExists(settingsPath); err != nil {
		return err
	}
	if err := patchSettingsAtPath(settingsPath, true); err != nil {
		return err
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
	if err := writeFileAtomic(paths.ClaudeTokenFile, []byte(installResp.ClaudeSessionToken), 0o600); err != nil {
		return err
	}

	code, err := createPairingCode(client, baseURL, installResp.InstallationToken)
	if err != nil {
		return fmt.Errorf("failed to create watch pairing code")
	}

	fmt.Println()
	fmt.Println(code)
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

func runEmit(args []string) error {
	fs := flag.NewFlagSet("emit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	action := fs.String("action", "", "hook action")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: taphapticctl emit --action <stop|subagent_stop|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention>")
	}
	if fs.NArg() != 0 || strings.TrimSpace(*action) == "" {
		return fmt.Errorf("usage: taphapticctl emit --action <stop|subagent_stop|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention>")
	}

	payload, err := eventForAction(*action)
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

	claudeToken := strings.TrimSpace(os.Getenv("TAPHAPTIC_CLAUDE_SESSION_TOKEN"))
	if claudeToken == "" {
		claudeToken = readTrimmedFile(paths.ClaudeTokenFile)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	if claudeToken == "" {
		installationToken := readTrimmedFile(paths.InstallationTokenFile)
		if installationToken != "" {
			if installResp, resolveErr := createInstallation(client, baseURL, installationToken); resolveErr == nil {
				claudeToken = strings.TrimSpace(installResp.ClaudeSessionToken)
				if claudeToken != "" {
					_ = writeFileAtomic(paths.ClaudeTokenFile, []byte(claudeToken), 0o600)
				}
			}
		}
	}

	if claudeToken == "" {
		return nil
	}

	_ = postEvent(client, baseURL, claudeToken, payload)
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
	var resp installationResponse
	status, body, err := postJSON(client, joinURL(baseURL, "/v1/claude/installations"), bearer, map[string]any{}, &resp)
	if err != nil {
		return installationResponse{}, err
	}
	if status != http.StatusOK {
		return installationResponse{}, fmt.Errorf("status=%d body=%s", status, strings.TrimSpace(string(body)))
	}
	if strings.TrimSpace(resp.InstallationToken) == "" || strings.TrimSpace(resp.InstallationID) == "" || strings.TrimSpace(resp.ClaudeSessionToken) == "" {
		return installationResponse{}, errors.New("missing installation fields")
	}
	return resp, nil
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
		"code":               code,
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

	if out != nil && len(bytes.TrimSpace(resBody)) > 0 {
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
	if out != nil && len(bytes.TrimSpace(body)) > 0 {
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
		APIBaseURLFile:        filepath.Join(installRoot, "api-base-url"),
		InstallationTokenFile: filepath.Join(installRoot, "installation-token"),
		InstallationIDFile:    filepath.Join(installRoot, "installation-id"),
		ClaudeTokenFile:       filepath.Join(installRoot, "claude-session-token"),
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
	return nil
}

func installHookWrapper(destination, ctlBinaryPath string) error {
	hookScript := "#!/bin/sh\n\nset -eu\n\naction=\"${1:-}\"\nexec " + strconv.Quote(ctlBinaryPath) + " emit --action \"$action\"\n"
	if err := writeFileAtomic(destination, []byte(hookScript), 0o755); err != nil {
		return err
	}
	return nil
}

func resolveSettingsPath(scope, cwd, homeDir string) (string, error) {
	switch strings.TrimSpace(scope) {
	case "user":
		return filepath.Join(homeDir, ".claude", "settings.json"), nil
	case "project":
		return filepath.Join(cwd, ".claude", "settings.json"), nil
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
	stopEntries = addCommand(stopEntries, "*", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" stop`)
	hooksConfig["Stop"] = stopEntries

	subagentEntries, err := ensureHookEntries(hooksConfig, "SubagentStop")
	if err != nil {
		return err
	}
	subagentEntries = pruneLegacyEntries(subagentEntries)
	subagentEntries = addCommand(subagentEntries, "*", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" subagent_stop`)
	hooksConfig["SubagentStop"] = subagentEntries

	if withNotifications {
		notificationEntries, err := ensureHookEntries(hooksConfig, "Notification")
		if err != nil {
			return err
		}
		notificationEntries = pruneLegacyEntries(notificationEntries)
		notificationEntries = addCommand(notificationEntries, "permission_prompt", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" permission_prompt`)
		notificationEntries = addCommand(notificationEntries, "idle_prompt", `/bin/sh "${HOME}/Library/Application Support/Taphaptic/bin/taphaptic-hook" idle_prompt`)
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
		return false
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
	switch strings.TrimSpace(action) {
	case "stop", "completed":
		return eventPayload{
			Type:   "completed",
			Source: "claude-code",
			Title:  "Claude Code completed",
			Body:   "AGENT COMPLETED A TASK",
		}, nil
	case "subagent_stop", "subagent_completed":
		return eventPayload{
			Type:   "subagent_completed",
			Source: "claude-code",
			Title:  "Claude subagent completed",
			Body:   "Claude Code subagent finished background work",
		}, nil
	case "failed":
		return eventPayload{
			Type:   "failed",
			Source: "claude-code",
			Title:  "Claude Code failed",
			Body:   "Claude Code reported a failure",
		}, nil
	case "permission_prompt":
		return eventPayload{
			Type:   "attention",
			Source: "claude-code",
			Title:  "Claude Code needs permission",
			Body:   "Claude Code is waiting for permission",
		}, nil
	case "idle_prompt":
		return eventPayload{
			Type:   "attention",
			Source: "claude-code",
			Title:  "Claude Code is waiting",
			Body:   "Claude Code is idle and waiting for input",
		}, nil
	case "attention":
		return eventPayload{
			Type:   "attention",
			Source: "claude-code",
			Title:  "Claude Code needs attention",
			Body:   "Claude Code needs attention",
		}, nil
	default:
		return eventPayload{}, fmt.Errorf("usage: taphapticctl emit --action <stop|subagent_stop|permission_prompt|idle_prompt|completed|subagent_completed|failed|attention>")
	}
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
