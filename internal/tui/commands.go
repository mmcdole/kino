package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/service"
)

// Command factories for async operations

// LoadLibrariesCmd loads all available libraries
func LoadLibrariesCmd(svc *service.LibraryService) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		libraries, err := svc.GetLibraries(ctx)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading libraries"}
		}
		return LibrariesLoadedMsg{Libraries: libraries}
	}
}

// LoadMoviesCmd loads movies from a library
func LoadMoviesCmd(svc *service.LibraryService, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // 60s for large libraries
		defer cancel()

		movies, err := svc.GetMovies(ctx, libID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading movies"}
		}
		return MoviesLoadedMsg{Movies: movies, LibraryID: libID}
	}
}

// LoadShowsCmd loads TV shows from a library
func LoadShowsCmd(svc *service.LibraryService, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // 60s for large libraries
		defer cancel()

		shows, err := svc.GetShows(ctx, libID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading shows"}
		}
		return ShowsLoadedMsg{Shows: shows, LibraryID: libID}
	}
}

// LoadMixedLibraryCmd loads content (movies AND shows) from a mixed library
func LoadMixedLibraryCmd(svc *service.LibraryService, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		items, err := svc.GetLibraryContent(ctx, libID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading library content"}
		}
		return MixedLibraryLoadedMsg{Items: items, LibraryID: libID}
	}
}

// LoadSeasonsCmd loads seasons for a show
func LoadSeasonsCmd(svc *service.LibraryService, showID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		seasons, err := svc.GetSeasons(ctx, showID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading seasons"}
		}
		return SeasonsLoadedMsg{Seasons: seasons, ShowID: showID}
	}
}

// LoadEpisodesCmd loads episodes for a season
func LoadEpisodesCmd(svc *service.LibraryService, seasonID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		episodes, err := svc.GetEpisodes(ctx, seasonID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading episodes"}
		}
		return EpisodesLoadedMsg{Episodes: episodes, SeasonID: seasonID}
	}
}

// PlayItemCmd starts playback of an item
func PlayItemCmd(svc *service.PlaybackService, item domain.MediaItem, resume bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		var err error
		if resume {
			err = svc.Resume(ctx, item)
		} else {
			err = svc.Play(ctx, item)
		}

		if err != nil {
			return ErrMsg{Err: err, Context: "starting playback"}
		}
		return PlaybackStartedMsg{Item: item}
	}
}

// MarkWatchedCmd marks an item as watched
func MarkWatchedCmd(svc *service.PlaybackService, itemID, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.MarkWatched(ctx, itemID); err != nil {
			return ErrMsg{Err: err, Context: "marking as watched"}
		}
		return MarkWatchedMsg{Title: title}
	}
}

// MarkUnwatchedCmd marks an item as unwatched
func MarkUnwatchedCmd(svc *service.PlaybackService, itemID, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.MarkUnwatched(ctx, itemID); err != nil {
			return ErrMsg{Err: err, Context: "marking as unwatched"}
		}
		return MarkUnwatchedMsg{Title: title}
	}
}

// TickCmd returns a command that sends a tick after a delay
func TickCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// ClearStatusCmd returns a command that clears status after a delay
func ClearStatusCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return ClearStatusMsg{}
	})
}

// ClearLibraryStatusCmd returns a command that clears library status after delay
func ClearLibraryStatusCmd(libID string, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return ClearLibraryStatusMsg{LibraryID: libID}
	})
}

// SyncLibraryCmd performs smart sync with streaming progress updates using channels
// Uses a continuation pattern to pump all progress messages to the UI
func SyncLibraryCmd(
	libSvc *service.LibraryService,
	lib domain.Library,
	force bool,
) tea.Cmd {
	return func() tea.Msg {
		// Use a generous timeout instead of Background
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

		// Create a channel for this sync operation
		progressCh := make(chan service.SyncProgress)

		// Start the background work
		go func() {
			defer cancel()
			libSvc.SyncLibrary(ctx, lib, force, progressCh)
		}()

		// Read the first message and return it with continuation context
		return readSyncProgress(lib, progressCh)
	}
}

// readSyncProgress reads one message from the channel and creates a LibrarySyncProgressMsg
// with the continuation command embedded
func readSyncProgress(
	lib domain.Library,
	progressCh <-chan service.SyncProgress,
) tea.Msg {
	progress, ok := <-progressCh
	if !ok {
		// Channel closed unexpectedly - sync was cancelled or errored
		return LibrarySyncProgressMsg{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Done:        true,
			Error:       fmt.Errorf("sync cancelled"),
		}
	}

	msg := LibrarySyncProgressMsg{
		LibraryID:   progress.LibraryID,
		LibraryType: progress.LibraryType,
		Loaded:      progress.Loaded,
		Total:       progress.Total,
		Done:        progress.Done,
		FromDisk:    progress.FromDisk,
		Error:       progress.Error,
	}

	// If not done and no error, attach continuation command
	if !progress.Done && progress.Error == nil {
		msg.NextCmd = listenToSyncCmd(lib, progressCh)
	}

	return msg
}

// listenToSyncCmd returns a command that reads the next message from the progress channel
func listenToSyncCmd(
	lib domain.Library,
	progressCh <-chan service.SyncProgress,
) tea.Cmd {
	return func() tea.Msg {
		return readSyncProgress(lib, progressCh)
	}
}

// SyncAllLibrariesCmd syncs all libraries in parallel
func SyncAllLibrariesCmd(
	libSvc *service.LibraryService,
	libraries []domain.Library,
	force bool,
) tea.Cmd {
	cmds := make([]tea.Cmd, len(libraries))
	for i, lib := range libraries {
		cmds[i] = SyncLibraryCmd(libSvc, lib, force)
	}
	return tea.Batch(cmds...)
}

// LogoutCmd clears server config and cache, then signals completion
func LogoutCmd() tea.Cmd {
	return func() tea.Msg {
		if err := config.ClearServerConfig(); err != nil {
			return LogoutCompleteMsg{Error: err}
		}
		if err := config.ClearCache(); err != nil {
			return LogoutCompleteMsg{Error: err}
		}
		return LogoutCompleteMsg{Error: nil}
	}
}

// LoadPlaylistsCmd loads all playlists
func LoadPlaylistsCmd(svc *service.PlaylistService) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlists, err := svc.GetPlaylists(ctx)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlists"}
		}
		return PlaylistsLoadedMsg{Playlists: playlists}
	}
}

// LoadPlaylistItemsCmd loads items from a playlist
func LoadPlaylistItemsCmd(svc *service.PlaylistService, playlistID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		items, err := svc.GetPlaylistItems(ctx, playlistID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlist items"}
		}
		return PlaylistItemsLoadedMsg{Items: items, PlaylistID: playlistID}
	}
}

// CreatePlaylistCmd creates a new playlist
func CreatePlaylistCmd(svc *service.PlaylistService, title string, itemIDs []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlist, err := svc.CreatePlaylist(ctx, title, itemIDs)
		if err != nil {
			return PlaylistCreatedMsg{Error: err}
		}
		return PlaylistCreatedMsg{Playlist: playlist}
	}
}

// AddToPlaylistCmd adds items to a playlist
func AddToPlaylistCmd(svc *service.PlaylistService, playlistID string, itemIDs []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := svc.AddToPlaylist(ctx, playlistID, itemIDs)
		if err != nil {
			return PlaylistUpdatedMsg{PlaylistID: playlistID, Error: err}
		}
		return PlaylistUpdatedMsg{PlaylistID: playlistID}
	}
}

// RemoveFromPlaylistCmd removes an item from a playlist
func RemoveFromPlaylistCmd(svc *service.PlaylistService, playlistID, itemID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := svc.RemoveFromPlaylist(ctx, playlistID, itemID)
		if err != nil {
			return PlaylistUpdatedMsg{PlaylistID: playlistID, Error: err}
		}
		return PlaylistUpdatedMsg{PlaylistID: playlistID}
	}
}

// DeletePlaylistCmd deletes a playlist
func DeletePlaylistCmd(svc *service.PlaylistService, playlistID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := svc.DeletePlaylist(ctx, playlistID)
		if err != nil {
			return PlaylistDeletedMsg{PlaylistID: playlistID, Error: err}
		}
		return PlaylistDeletedMsg{PlaylistID: playlistID}
	}
}

// LoadPlaylistModalDataCmd loads data for the playlist management modal
func LoadPlaylistModalDataCmd(svc *service.PlaylistService, item *domain.MediaItem) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlists, err := svc.GetPlaylists(ctx)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlists for modal"}
		}

		membership, err := svc.GetPlaylistMembership(ctx, item.ID)
		if err != nil {
			return ErrMsg{Err: err, Context: "checking playlist membership"}
		}

		return PlaylistModalDataMsg{
			Playlists:  playlists,
			Membership: membership,
			Item:       item,
		}
	}
}
