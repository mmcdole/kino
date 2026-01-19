package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/drake/goplex/internal/adapter"
	"github.com/drake/goplex/internal/adapter/source/plex"
	"github.com/drake/goplex/internal/service"
	"github.com/drake/goplex/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := adapter.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger
	logger, err := adapter.SetupLogger(&cfg.Logging)
	if err != nil {
		// Fall back to null logger if file logging fails
		logger = adapter.NullLogger()
	}
	slog.SetDefault(logger)

	logger.Info("starting goplex", "version", "1.0.0")

	// Check if configured
	if !cfg.IsConfigured() {
		return runAuthFlow(cfg, logger)
	}

	// Create adapters
	plexClient := plex.NewClient(cfg.Server.URL, cfg.Server.Token, logger)

	// Create launcher (uses configured player or system default)
	launcher := adapter.NewLauncher(cfg.Player.Command, cfg.Player.Args, logger)

	// Create services
	librarySvc := service.NewLibraryService(plexClient, logger)
	searchSvc := service.NewSearchService(plexClient, logger)
	playbackSvc := service.NewPlaybackService(launcher, plexClient, logger)

	// Create TUI model
	model := tui.NewModel(librarySvc, playbackSvc, searchSvc)

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

// runAuthFlow handles the initial authentication when not configured
func runAuthFlow(cfg *adapter.Config, logger *slog.Logger) error {
	fmt.Println("Welcome to Goplex!")
	fmt.Println()

	// Get server URL if not set
	if cfg.Server.URL == "" {
		fmt.Print("Enter your Plex server URL (e.g., http://192.168.1.100:32400): ")
		var url string
		fmt.Scanln(&url)
		cfg.Server.URL = url
	}

	// Start PIN auth flow
	authClient := plex.NewAuthClient(logger)
	ctx := context.Background()

	fmt.Println()
	fmt.Println("Generating authentication PIN...")

	pin, pinID, err := authClient.GetPIN(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate PIN: %w", err)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Go to: https://plex.tv/link\n")
	fmt.Printf("  Enter PIN: %s\n", pin)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Waiting for authentication...")

	// Wait for PIN to be claimed (5 minutes timeout)
	token, err := authClient.WaitForPIN(ctx, pinID, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Authentication successful!")

	// Save token
	cfg.Server.Token = token
	if err := adapter.SaveConfig(cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println("✓ Configuration saved!")
	fmt.Println()
	fmt.Println("Run goplex again to start the application.")

	return nil
}
