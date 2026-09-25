package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// clearSpinnerLine clears the spinner line from the terminal
const clearSpinnerLine = "\r                                    \r"

// runSetupFlow handles the initial setup when not configured
func runSetupFlow(cfg *config.Config, logger *slog.Logger) error {
	fmt.Println()
	fmt.Println("Welcome to Kino!")
	fmt.Println()

	// Loop until we get a valid server URL
	var serverURL string
	var serverType config.SourceType

	for {
		// Prompt for server URL
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Enter your server URL (e.g., http://192.168.1.100:32400): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		serverURL = strings.TrimSpace(input)

		if serverURL == "" {
			fmt.Println("Server URL cannot be empty. Please try again.")
			continue
		}

		// Scheme-less input ("192.168.1.100:32400") would otherwise die with
		// a cryptic "unsupported protocol scheme" error
		if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
			serverURL = "http://" + serverURL
		}

		// Detect server type with spinner
		fmt.Println()
		detectedType, err := detectServerWithSpinner(serverURL)
		if err != nil {
			fmt.Printf("\n✗ Could not detect server type: %v\n", err)
			fmt.Println("Please check the URL and try again.")
			fmt.Println()
			continue
		}

		serverType = detectedType
		break
	}

	// Update config with server info
	cfg.Server.URL = serverURL
	cfg.Server.Type = serverType

	// Ctrl+C during the (up to 5-minute) PIN wait should cancel cleanly
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var result *domain.Credentials
	var err error
	switch serverType {
	case config.SourceTypePlex:
		result, err = signInPlex(ctx, cfg.Server.DeviceID, logger)
	case config.SourceTypeJellyfin:
		result, err = signInJellyfin(ctx, serverURL, cfg.Server.DeviceID, logger)
	default:
		err = fmt.Errorf("unknown server type: %s", serverType)
	}
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Save credentials
	cfg.Server.Token = result.Token
	cfg.Server.UserID = result.UserID
	cfg.Server.Username = result.Username

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Configuration saved! Starting kino...")

	return nil
}

// detectServerWithSpinner detects the server type with a visual spinner
func detectServerWithSpinner(serverURL string) (config.SourceType, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Channel to receive result
	type result struct {
		serverType config.SourceType
		err        error
	}
	resultCh := make(chan result, 1)

	// Start detection in background
	go func() {
		serverType, err := mediaserver.DetectServerType(ctx, serverURL)
		resultCh <- result{serverType, err}
	}()

	// Spinner animation
	frame := 0

	// Print initial spinner
	fmt.Printf("\r%s Detecting server type...", styles.SpinnerFrames[frame])

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case res := <-resultCh:
			// Clear spinner line
			fmt.Print(clearSpinnerLine)

			if res.err != nil {
				return "", res.err
			}

			// Show success with server type
			serverName := "Unknown"
			switch res.serverType {
			case config.SourceTypePlex:
				serverName = "Plex Media Server"
			case config.SourceTypeJellyfin:
				serverName = "Jellyfin"
			}
			fmt.Printf("✓ Detected: %s\n", serverName)

			return res.serverType, nil

		case <-ticker.C:
			frame++
			fmt.Printf("\r%s Detecting server type...", styles.SpinnerFrames[frame%len(styles.SpinnerFrames)])

		case <-ctx.Done():
			fmt.Print(clearSpinnerLine)
			return "", fmt.Errorf("detection timed out")
		}
	}
}

// signInPlex links this install through plex.tv: the user enters a PIN at
// plex.tv/link and the token arrives once it is claimed.
func signInPlex(ctx context.Context, deviceID string, logger *slog.Logger) (*domain.Credentials, error) {
	auth := plex.NewAuthClient(deviceID, logger)

	fmt.Println()
	fmt.Println("Generating authentication PIN...")

	pin, pinID, err := auth.GetPIN(ctx)
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

	token, err := auth.WaitForPIN(ctx, pinID, 5*time.Minute)
	if err != nil {
		return nil, err
	}

	fmt.Println()
	fmt.Println("Authentication successful!")

	// Plex API calls need only the token.
	return &domain.Credentials{Token: token}, nil
}

// signInJellyfin prompts for a username and password and signs in.
func signInJellyfin(ctx context.Context, serverURL, deviceID string, logger *slog.Logger) (*domain.Credentials, error) {
	fmt.Println()
	fmt.Println("Jellyfin Authentication")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Username: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read username: %w", err)
	}
	username = strings.TrimSpace(username)

	fmt.Print("Password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Println() // hidden input leaves the cursor on the prompt line

	fmt.Println()
	fmt.Println("Authenticating...")

	result, err := jellyfin.Authenticate(ctx, serverURL, deviceID, username, string(passwordBytes), logger)
	if err != nil {
		return nil, err
	}

	fmt.Println()
	fmt.Println("Authentication successful!")
	return result, nil
}
