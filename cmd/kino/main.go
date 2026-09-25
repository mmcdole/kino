package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/mediaserver"
	"github.com/mmcdole/kino/internal/player"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/store"
	"github.com/mmcdole/kino/internal/tui"
)

// Version is set at build time via -ldflags
var Version = "dev"

// snapshotCache is the catalog's cache plus the shutdown hook main needs.
type snapshotCache interface {
	catalog.Cache
	io.Closer
}

// session signs out by clearing the credentials in the loaded config file.
type session struct{ cfg *config.Config }

func (s session) Logout() error { return s.cfg.ClearServer() }

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
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger
	logger, err := openLogger(&cfg.Logging)
	if err != nil {
		// Fall back to null logger if file logging fails
		logger = slog.New(slog.DiscardHandler)
	}
	slog.SetDefault(logger)

	logger.Info("starting kino", "version", Version)

	// Check if configured
	if !cfg.IsConfigured() {
		if err := runSetupFlow(cfg, logger); err != nil {
			return err
		}
		// Fall through into normal startup with the fresh credentials —
		// no need to make the user run kino a second time
	}

	// Create media source client
	client, err := mediaserver.New(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create media client: %w", err)
	}

	// The disk cache enables offline browsing; without it, cache in memory.
	var cache snapshotCache
	if disk, err := store.OpenDisk(store.Dir(cfg.Server.URL, cfg.Server.UserID)); err == nil {
		cache = disk
	} else {
		logger.Warn("store unavailable, continuing memory-only", "error", err)
		cache = store.NewMemory()
	}
	defer cache.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	catalogSvc := catalog.NewService(ctx, client, cache)
	defer catalogSvc.Close()
	playback := player.New(cfg.Player.Command, cfg.Player.Args, cfg.Player.StartFlag, client, logger)
	opts := tui.Options{ShowWatchStatus: cfg.UI.ShowWatchStatus, ShowLibraryCounts: cfg.UI.ShowLibraryCounts}
	model := tui.NewModel(ctx, catalogSvc, playback, session{cfg}, search.NewIndex(), opts)

	// Run the TUI
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	logger.Info("starting TUI")

	result, err := p.Run()
	if err != nil {
		logger.Error("TUI error", "error", err)
		return fmt.Errorf("TUI error: %w", err)
	}

	cancel()
	catalogSvc.Close()
	if final, ok := result.(*tui.Model); ok && final.LoggedOut {
		if err := cache.Close(); err != nil {
			return err
		}
		if err := store.Clear(); err != nil {
			return err
		}
	}
	logger.Info("shutting down")
	return nil
}
