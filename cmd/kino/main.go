package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/log"
	"github.com/mmcdole/kino/internal/mediaserver"
	"github.com/mmcdole/kino/internal/player"
	"github.com/mmcdole/kino/internal/service"
	"github.com/mmcdole/kino/internal/tui"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// Version is set at build time via -ldflags
var Version = "dev"

// clearSpinnerLine clears the spinner line from the terminal
const clearSpinnerLine = "\r                                    \r"

func main() {
	// Handle version flag
	var showVersion bool
	flag.BoolVar(&showVersion, "v", false, "print version")
	flag.BoolVar(&showVersion, "version", false, "print version")
	flag.Parse()

	if showVersion {
		fmt.Printf("kino %s\n", Version)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger
	logger, err := log.SetupLogger(&cfg.Logging)
	if err != nil {
		// Fall back to null logger if file logging fails
		logger = log.NullLogger()
	}
	slog.SetDefault(logger)

	logger.Info("starting kino", "version", Version)

	// Check if configured
	if !cfg.IsConfigured() {
		return runSetupFlow(cfg, logger)
	}

	// Create media source client
	client, err := mediaserver.NewClient(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create media client: %w", err)
	}

	// Create launcher (uses configured player or auto-detects)
	launcher := player.NewLauncher(cfg.Player.Command, cfg.Player.Args, cfg.Player.StartFlag, logger)

	// Create services
	librarySvc := service.NewLibraryService(client, logger, cfg.Server.URL)
	searchSvc := service.NewSearchService(client, librarySvc, logger)
	playbackSvc := service.NewPlaybackService(launcher, client, logger)
	playlistSvc := service.NewPlaylistService(client, logger)

	// Create TUI model
	model := tui.NewModel(librarySvc, playbackSvc, searchSvc, playlistSvc)

	// Run the TUI
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	logger.Info("starting TUI")

	if _, err := p.Run(); err != nil {
		logger.Error("TUI error", "error", err)
		return fmt.Errorf("TUI error: %w", err)
	}

	logger.Info("shutting down")
	return nil
}

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

	// Run the appropriate auth flow
	authFlow, err := mediaserver.NewAuthFlow(serverType, logger)
	if err != nil {
		return fmt.Errorf("failed to create auth flow: %w", err)
	}

	ctx := context.Background()
	result, err := authFlow.Run(ctx, serverURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Save credentials
	cfg.Server.Token = result.Token
	cfg.Server.UserID = result.UserID
	cfg.Server.Username = result.Username

	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Configuration saved!")
	fmt.Println()
	fmt.Println("Run kino again to start the application.")

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
