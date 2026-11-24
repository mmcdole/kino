package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

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

	if name := flag.Arg(0); menuCommands[name] {
		if err := runMenuCommand(name, flag.Args()[1:]); err != nil {
			reportError(err)
			os.Exit(1)
		}
		return
	}

	if flag.Arg(0) == player.SessionCommand {
		if err := runPlaySession(); err != nil {
			slog.Error("play session failed", "error", err)
			os.Exit(1)
		}
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

	a, err := newApp(cfg, logger)
	if err != nil {
		return err
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	catalogSvc, playback, cache := a.catalog, a.playback, a.cache
	opts := tui.Options{
		ShowWatchStatus:   cfg.UI.ShowWatchStatus,
		ShowLibraryCounts: cfg.UI.ShowLibraryCounts,
		NoPlaylists:       !catalogSvc.HasPlaylists(),
		FetchSummaries:    catalogSvc.FetchesSummaries(),
	}
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

// app is a configured server client with its cache, catalog and player.
type app struct {
	cfg      *config.Config
	client   mediaserver.Server
	cache    snapshotCache
	catalog  *catalog.Service
	playback *player.Player
	cancel   context.CancelFunc
}

// newApp connects the configured server to the cache, catalog and player.
func newApp(cfg *config.Config, logger *slog.Logger) (*app, error) {
	client, err := mediaserver.New(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create media client: %w", err)
	}
	// The disk cache enables offline browsing; without it, cache in memory.
	var cache snapshotCache
	if disk, err := store.OpenDisk(store.Dir(cfg.Server.URL, cfg.Server.UserID)); err == nil {
		cache = disk
	} else {
		logger.Warn("store unavailable, continuing memory-only", "error", err)
		cache = store.NewMemory()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &app{
		cfg:      cfg,
		client:   client,
		cache:    cache,
		catalog:  catalog.NewService(ctx, client, cache),
		playback: player.New(cfg.Player.Command, cfg.Player.Args, cfg.Player.StartFlag, client, logger),
		cancel:   cancel,
	}, nil
}

// openApp loads the config and opens the app for a command that can't run
// kino's interactive sign-in.
func openApp() (*app, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	logger, err := openLogger(&cfg.Logging)
	if err != nil {
		logger = slog.New(slog.DiscardHandler)
	}
	slog.SetDefault(logger)
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("not signed in: run kino once to set up a server")
	}
	return newApp(cfg, logger)
}

// Close stops the catalog's work and closes the cache. Closing twice is safe.
func (a *app) Close() error {
	a.cancel()
	a.catalog.Close()
	return a.cache.Close()
}

// runPlaySession follows one play the TUI started, from the player's launch
// to its exit, reporting progress to the server. The TUI starts it with the
// play on stdin; it outlives the TUI as the player does.
func runPlaySession() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	logger, err := openLogger(&cfg.Logging)
	if err != nil {
		logger = slog.New(slog.DiscardHandler)
	}
	slog.SetDefault(logger)
	logger = logger.With("component", "play-session")

	client, err := mediaserver.New(cfg, logger)
	if err != nil {
		fmt.Println("error " + err.Error())
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	playback := player.New(cfg.Player.Command, cfg.Player.Args, cfg.Player.StartFlag, client, logger)
	return playback.RunSession(ctx, os.Stdin, os.Stdout)
}
