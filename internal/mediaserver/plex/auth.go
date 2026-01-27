package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// AuthResult contains the result of a successful Plex authentication
type AuthResult struct {
	Token    string
	UserID   string
	Username string
}

// Plex-specific errors
var (
	// ErrPINExpired indicates the authentication PIN has expired
	ErrPINExpired = errors.New("authentication PIN has expired")
	// ErrServerOffline indicates the media server is unreachable
	ErrServerOffline = errors.New("media server is unreachable")
)

const (
	plexTVBaseURL = "https://plex.tv"
	pinEndpoint   = "/api/v2/pins"
)

// AuthClient handles Plex authentication
type AuthClient struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAuthClient creates a new authentication client
func NewAuthClient(logger *slog.Logger) *AuthClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// GetPIN generates a new authentication PIN
func (a *AuthClient) GetPIN(ctx context.Context) (pin string, id int, err error) {
	reqURL := fmt.Sprintf("%s%s", plexTVBaseURL, pinEndpoint)

	data := url.Values{}
	data.Set("strong", "false")
	data.Set("X-Plex-Product", "Kino")
	data.Set("X-Plex-Client-Identifier", clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	// Add data as query params for POST
	req.URL.RawQuery = data.Encode()

	a.logger.Debug("requesting PIN", "url", reqURL)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.logger.Error("PIN request failed", "error", err)
		return "", 0, ErrServerOffline
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		a.logger.Error("PIN request error", "status", resp.StatusCode, "body", string(body))
		return "", 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var pinResp PINResponse
	if err := json.Unmarshal(body, &pinResp); err != nil {
		return "", 0, fmt.Errorf("failed to parse PIN response: %w", err)
	}

	a.logger.Info("PIN generated", "pin", pinResp.Code, "id", pinResp.ID)
	return pinResp.Code, pinResp.ID, nil
}

// CheckPIN polls for PIN claim status and returns the auth token
func (a *AuthClient) CheckPIN(ctx context.Context, pinID int) (token string, claimed bool, err error) {
	reqURL := fmt.Sprintf("%s%s/%d", plexTVBaseURL, pinEndpoint, pinID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	req.Header.Set("X-Plex-Product", "Kino")
	req.Header.Set("X-Plex-Version", "1.0")
	req.Header.Set("User-Agent", userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.logger.Error("PIN check failed", "error", err)
		return "", false, ErrServerOffline
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", false, ErrPINExpired
	}

	if resp.StatusCode != http.StatusOK {
		a.logger.Error("PIN check error", "status", resp.StatusCode, "body", string(body))
		return "", false, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var pinResp PINCheckResponse
	if err := json.Unmarshal(body, &pinResp); err != nil {
		return "", false, fmt.Errorf("failed to parse PIN response: %w", err)
	}

	if pinResp.AuthToken == "" {
		return "", false, nil // Not yet claimed
	}

	a.logger.Info("PIN claimed successfully")
	return pinResp.AuthToken, true, nil
}

// WaitForPIN polls for PIN claim with exponential backoff
func (a *AuthClient) WaitForPIN(ctx context.Context, pinID int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	interval := 1 * time.Second
	maxInterval := 5 * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
			token, claimed, err := a.CheckPIN(ctx, pinID)
			if err != nil {
				if err == ErrPINExpired {
					return "", err
				}
				a.logger.Warn("PIN check error, retrying", "error", err)
				continue
			}

			if claimed {
				return token, nil
			}

			// Increase interval up to max
			interval = min(interval*2, maxInterval)
		}
	}

	return "", ErrPINExpired
}

// AuthFlow handles Plex PIN-based authentication
type AuthFlow struct {
	client *AuthClient
}

// NewAuthFlow creates a new Plex authentication flow
func NewAuthFlow(logger *slog.Logger) *AuthFlow {
	return &AuthFlow{
		client: NewAuthClient(logger),
	}
}

// Run executes the Plex PIN-based authentication flow.
// It prompts the user to visit plex.tv/link and enter the displayed PIN.
func (f *AuthFlow) Run(ctx context.Context, serverURL string) (*AuthResult, error) {
	// Note: serverURL is not used for Plex auth since authentication
	// happens via plex.tv, not the local server

	fmt.Println()
	fmt.Println("Generating authentication PIN...")

	pin, pinID, err := f.client.GetPIN(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate PIN: %w", err)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Go to: https://plex.tv/link\n")
	fmt.Printf("  Enter PIN: %s\n", pin)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Waiting for authentication...")

	// Wait for PIN to be claimed (5 minutes timeout)
	token, err := f.client.WaitForPIN(ctx, pinID, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println()
	fmt.Println("Authentication successful!")

	return &AuthResult{
		Token: token,
		// Plex doesn't require UserID for API calls
		UserID:   "",
		Username: "", // Could fetch from /api/v2/user but not needed
	}, nil
}
