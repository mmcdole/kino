package tui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/service"
)

// syncChannelSize is the buffer size for sync progress channels
const syncChannelSize = 100

// Command factories for async operations

// LoadLibrariesCmd loads all available libraries
func LoadLibrariesCmd(cmds domain.LibraryCommands) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		libraries, err := cmds.FetchLibraries(ctx)
		if err != nil {
			slog.Error("failed to load libraries", "error", err)
			return ErrMsg{Err: err, Context: "loading libraries"}
		}
		return LibrariesLoadedMsg{Libraries: libraries}
	}
}

// LoadMoviesCmd loads movies from a library
func LoadMoviesCmd(cmds domain.LibraryCommands, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		movies, err := cmds.FetchMovies(ctx, libID, nil)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading movies"}
		}
		return MoviesLoadedMsg{Movies: movies, LibraryID: libID}
	}
}

// LoadShowsCmd loads TV shows from a library
func LoadShowsCmd(cmds domain.LibraryCommands, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		shows, err := cmds.FetchShows(ctx, libID, nil)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading shows"}
		}
		return ShowsLoadedMsg{Shows: shows, LibraryID: libID}
	}
}

// LoadMixedLibraryCmd loads content (movies AND shows) from a mixed library
func LoadMixedLibraryCmd(cmds domain.LibraryCommands, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		items, err := cmds.FetchMixedContent(ctx, libID, nil)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading library content"}
		}
		return MixedLibraryLoadedMsg{Items: items, LibraryID: libID}
	}
}

// LoadSeasonsCmd loads seasons for a show
func LoadSeasonsCmd(cmds domain.LibraryCommands, libID, showID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		seasons, err := cmds.FetchSeasons(ctx, libID, showID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading seasons"}
		}
		return SeasonsLoadedMsg{Seasons: seasons, ShowID: showID}
	}
}

// LoadEpisodesCmd loads episodes for a season
func LoadEpisodesCmd(cmds domain.LibraryCommands, libID, showID, seasonID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		episodes, err := cmds.FetchEpisodes(ctx, libID, showID, seasonID)
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

// SyncLibraryCmd performs smart sync with streaming progress updates
func SyncLibraryCmd(cmds domain.LibraryCommands, lib domain.Library) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

		progressCh := make(chan syncProgress, syncChannelSize)

		go func() {
			defer cancel()
			defer close(progressCh)

			onProgress := func(loaded, total int) {
				select {
				case progressCh <- syncProgress{loaded: loaded, total: total}:
				default:
				}
			}

			result, err := cmds.SyncLibrary(ctx, lib, onProgress)

			// Send final message
			select {
			case progressCh <- syncProgress{
				loaded:    result.Count,
				total:     result.Count,
				done:      true,
				fromCache: result.FromCache,
				err:       err,
			}:
			default:
			}
		}()

		return readSyncProgress(lib, progressCh)
	}
}

// syncProgress is an internal type for channel communication
type syncProgress struct {
	loaded    int
	total     int
	done      bool
	fromCache bool
	err       error
}

// readSyncProgress reads one message from the channel and creates a LibrarySyncProgressMsg
func readSyncProgress(lib domain.Library, progressCh <-chan syncProgress) tea.Msg {
	progress, ok := <-progressCh
	if !ok {
		return LibrarySyncProgressMsg{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Done:        true,
			Error:       fmt.Errorf("sync cancelled"),
		}
	}

	msg := LibrarySyncProgressMsg{
		LibraryID:   lib.ID,
		LibraryType: lib.Type,
		Loaded:      progress.loaded,
		Total:       progress.total,
		Done:        progress.done,
		FromCache:   progress.fromCache,
		Error:       progress.err,
	}

	if !progress.done && progress.err == nil {
		msg.NextCmd = listenToSyncCmd(lib, progressCh)
	}

	return msg
}

// listenToSyncCmd returns a command that reads the next message from the progress channel
func listenToSyncCmd(lib domain.Library, progressCh <-chan syncProgress) tea.Cmd {
	return func() tea.Msg {
		return readSyncProgress(lib, progressCh)
	}
}

// SyncAllLibrariesCmd syncs all libraries in parallel
func SyncAllLibrariesCmd(cmds domain.LibraryCommands, libraries []domain.Library) tea.Cmd {
	teaCmds := make([]tea.Cmd, len(libraries))
	for i, lib := range libraries {
		teaCmds[i] = SyncLibraryCmd(cmds, lib)
	}
	return tea.Batch(teaCmds...)
}

// SyncPlaylistsCmd syncs playlists and their items (two levels deep, like library sync).
func SyncPlaylistsCmd(cmds domain.PlaylistCommands, playlistsID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

		progressCh := make(chan syncProgress, syncChannelSize)

		go func() {
			defer cancel()
			defer close(progressCh)

			// SyncPlaylists fetches playlists AND items for each
			playlists, err := cmds.SyncPlaylists(ctx)

			// Send final message
			select {
			case progressCh <- syncProgress{
				loaded:    len(playlists),
				total:     len(playlists),
				done:      true,
				fromCache: false,
				err:       err,
			}:
			default:
			}
		}()

		return readPlaylistSyncProgress(playlistsID, progressCh)
	}
}

// readPlaylistSyncProgress reads sync progress for playlists
func readPlaylistSyncProgress(playlistsID string, progressCh <-chan syncProgress) tea.Msg {
	progress, ok := <-progressCh
	if !ok {
		return LibrarySyncProgressMsg{
			LibraryID:   playlistsID,
			LibraryType: "playlist",
			Done:        true,
			Error:       fmt.Errorf("sync cancelled"),
		}
	}

	return LibrarySyncProgressMsg{
		LibraryID:   playlistsID,
		LibraryType: "playlist",
		Loaded:      progress.loaded,
		Total:       progress.total,
		Done:        progress.done,
		FromCache:   progress.fromCache,
		Error:       progress.err,
	}
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
func LoadPlaylistsCmd(cmds domain.PlaylistCommands) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlists, err := cmds.FetchPlaylists(ctx)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlists"}
		}
		return PlaylistsLoadedMsg{Playlists: playlists}
	}
}

// LoadPlaylistItemsCmd loads items from a playlist
func LoadPlaylistItemsCmd(cmds domain.PlaylistCommands, playlistID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		items, err := cmds.FetchPlaylistItems(ctx, playlistID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlist items"}
		}
		return PlaylistItemsLoadedMsg{Items: items, PlaylistID: playlistID}
	}
}

// CreatePlaylistCmd creates a new playlist
func CreatePlaylistCmd(cmds domain.PlaylistCommands, title string, itemIDs []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlist, err := cmds.CreatePlaylist(ctx, title, itemIDs)
		if err != nil {
			return PlaylistCreatedMsg{Error: err}
		}
		return PlaylistCreatedMsg{Playlist: playlist}
	}
}

// AddToPlaylistCmd adds items to a playlist
func AddToPlaylistCmd(cmds domain.PlaylistCommands, playlistID string, itemIDs []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := cmds.AddToPlaylist(ctx, playlistID, itemIDs)
		if err != nil {
			return PlaylistUpdatedMsg{PlaylistID: playlistID, Error: err}
		}
		return PlaylistUpdatedMsg{PlaylistID: playlistID}
	}
}

// RemoveFromPlaylistCmd removes an item from a playlist
func RemoveFromPlaylistCmd(cmds domain.PlaylistCommands, playlistID, itemID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := cmds.RemoveFromPlaylist(ctx, playlistID, itemID)
		if err != nil {
			return PlaylistUpdatedMsg{PlaylistID: playlistID, Error: err}
		}
		return PlaylistUpdatedMsg{PlaylistID: playlistID}
	}
}

// DeletePlaylistCmd deletes a playlist
func DeletePlaylistCmd(cmds domain.PlaylistCommands, playlistID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := cmds.DeletePlaylist(ctx, playlistID)
		if err != nil {
			return PlaylistDeletedMsg{PlaylistID: playlistID, Error: err}
		}
		return PlaylistDeletedMsg{PlaylistID: playlistID}
	}
}

// LoadPlaylistModalDataCmd loads data for the playlist management modal
func LoadPlaylistModalDataCmd(cmds domain.PlaylistCommands, item *domain.MediaItem) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlists, err := cmds.FetchPlaylists(ctx)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlists for modal"}
		}

		membership, err := cmds.GetPlaylistMembership(ctx, item.ID)
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
