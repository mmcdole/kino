package jellyfin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"golang.org/x/term"
)

const (
	authTimeout = 30 * time.Second
)

// AuthFlow implements domain.AuthFlow for Jellyfin username/password authentication
type AuthFlow struct {
	logger     *slog.Logger
	httpClient *http.Client
}

// NewAuthFlow creates a new Jellyfin authentication flow
func NewAuthFlow(logger *slog.Logger) *AuthFlow {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthFlow{
		logger: logger,
		httpClient: &http.Client{
			Timeout: authTimeout,
		},
	}
}

// Run executes the Jellyfin username/password authentication flow.
// It prompts the user for credentials and authenticates against the server.
func (f *AuthFlow) Run(ctx context.Context, serverURL string) (*domain.AuthResult, error) {
	serverURL = strings.TrimRight(serverURL, "/")

	fmt.Println()
	fmt.Println("Jellyfin Authentication")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━")

	// Prompt for username
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read username: %w", err)
	}
	username = strings.TrimSpace(username)

	// Prompt for password (hidden input)
	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}
	password := string(passwordBytes)
	fmt.Println() // Add newline after hidden input

	fmt.Println()
	fmt.Println("Authenticating...")

	// Authenticate
	result, err := f.authenticate(ctx, serverURL, username, password)
	if err != nil {
		return nil, err
	}

	fmt.Println()
	fmt.Println("Authentication successful!")

	return result, nil
}

// authenticate performs the actual authentication against the Jellyfin server
func (f *AuthFlow) authenticate(ctx context.Context, serverURL, username, password string) (*domain.AuthResult, error) {
	url := serverURL + "/Users/AuthenticateByName"

	// Build request body
	body := map[string]string{
		"Username": username,
		"Pw":       password,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Emby-Authorization", buildAuthHeader("", "")) // No token/userID yet

	resp, err := f.httpClient.Do(req)
	if err != nil {
		f.logger.Error("Jellyfin auth request failed", "error", err)
		return nil, domain.ErrServerOffline
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, domain.ErrAuthFailed
	}

	if resp.StatusCode != http.StatusOK {
		f.logger.Error("Jellyfin auth error", "status", resp.StatusCode, "body", string(respBody))
		return nil, fmt.Errorf("authentication failed with status %d", resp.StatusCode)
	}

	var authResp AuthResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return nil, fmt.Errorf("failed to parse auth response: %w", err)
	}

	return &domain.AuthResult{
		Token:    authResp.AccessToken,
		UserID:   authResp.User.ID,
		Username: authResp.User.Name,
	}, nil
}

// buildAuthHeader constructs the X-Emby-Authorization header
func buildAuthHeader(token, userID string) string {
	parts := []string{
		`MediaBrowser Client="Kino"`,
		`Device="CLI"`,
		`DeviceId="kino-tui-client"`,
		`Version="1.0.0"`,
	}

	if token != "" {
		parts = append(parts, fmt.Sprintf(`Token="%s"`, token))
	}

	return strings.Join(parts, ", ")
}

// PromptForServerURL prompts the user to enter a Jellyfin server URL
func PromptForServerURL() (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your Jellyfin server URL (e.g., http://192.168.1.100:8096): ")
	url, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return strings.TrimSpace(url), nil
}
