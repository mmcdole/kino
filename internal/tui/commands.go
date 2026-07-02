package tui

import (
	"context"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/library"
	"github.com/mmcdole/kino/internal/player"
	"github.com/mmcdole/kino/internal/playlist"
)

// syncChannelSize is the buffer size for sync progress channels
const syncChannelSize = 100

// Command factories for async operations

// LoadLibrariesCmd loads all available libraries
func LoadLibrariesCmd(svc *library.Service) tea.Cmd {
	return loadLibrariesCmd(svc, false)
}

// RefreshLibrariesCmd reloads libraries for refresh-all, signaling the
// handler to preserve the navigation stack where possible
func RefreshLibrariesCmd(svc *library.Service) tea.Cmd {
	return loadLibrariesCmd(svc, true)
}

func loadLibrariesCmd(svc *library.Service, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		libraries, err := svc.FetchLibraries(ctx)
		if err != nil {
			slog.Error("failed to load libraries", "error", err)
			return ErrMsg{Err: err, Context: "loading libraries"}
		}
		return LibrariesLoadedMsg{Libraries: libraries, Refresh: refresh}
	}
}

// LoadMoviesCmd loads movies from a library
func LoadMoviesCmd(svc *library.Service, lib domain.Library) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		movies, err := svc.FetchMovies(ctx, lib.ID, lib.UpdatedAt, nil)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading movies"}
		}
		return MoviesLoadedMsg{Movies: movies, LibraryID: lib.ID}
	}
}

// LoadShowsCmd loads TV shows from a library
func LoadShowsCmd(svc *library.Service, lib domain.Library) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		shows, err := svc.FetchShows(ctx, lib.ID, lib.UpdatedAt, nil)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading shows"}
		}
		return ShowsLoadedMsg{Shows: shows, LibraryID: lib.ID}
	}
}

// LoadMixedLibraryCmd loads content (movies AND shows) from a mixed library
func LoadMixedLibraryCmd(svc *library.Service, lib domain.Library) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		items, err := svc.FetchMixedContent(ctx, lib.ID, lib.UpdatedAt, nil)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading library content"}
		}
		return MixedLibraryLoadedMsg{Items: items, LibraryID: lib.ID}
	}
}

// LoadSeasonsCmd loads seasons for a show
func LoadSeasonsCmd(svc *library.Service, libID, showID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		seasons, err := svc.FetchSeasons(ctx, libID, showID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading seasons"}
		}
		return SeasonsLoadedMsg{Seasons: seasons, ShowID: showID}
	}
}

// LoadEpisodesCmd loads episodes for a season
func LoadEpisodesCmd(svc *library.Service, libID, showID, seasonID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		episodes, err := svc.FetchEpisodes(ctx, libID, showID, seasonID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading episodes"}
		}
		return EpisodesLoadedMsg{Episodes: episodes, SeasonID: seasonID}
	}
}

// PlayItemCmd starts playback of an item
func PlayItemCmd(svc *player.Service, item domain.MediaItem, resume bool) tea.Cmd {
	return func() tea.Msg {
		// URL resolution is a network round-trip; a hung server must not
		// wedge the command goroutine forever
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

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
func MarkWatchedCmd(svc *player.Service, itemID, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.MarkWatched(ctx, itemID); err != nil {
			return ErrMsg{Err: err, Context: "marking as watched"}
		}
		return MarkWatchedMsg{ItemID: itemID, Title: title}
	}
}

// MarkUnwatchedCmd marks an item as unwatched
func MarkUnwatchedCmd(svc *player.Service, itemID, title string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := svc.MarkUnwatched(ctx, itemID); err != nil {
			return ErrMsg{Err: err, Context: "marking as unwatched"}
		}
		return MarkUnwatchedMsg{ItemID: itemID, Title: title}
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

// SyncLibraryCmd performs smart sync with streaming progress updates.
// The generation tags every message so the model can drop chains superseded
// by a newer library reload (refresh-all during a running sync).
func SyncLibraryCmd(svc *library.Service, lib domain.Library, generation int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

		progressCh := make(chan syncProgress, syncChannelSize)
		// Dedicated 1-slot channel for the terminal message: progress updates
		// may be dropped under load, but the done/error result must not be —
		// and the buffered slot means a superseded goroutine never blocks.
		doneCh := make(chan syncProgress, 1)

		go func() {
			defer cancel()
			defer close(progressCh)

			onProgress := func(loaded, total int) {
				select {
				case progressCh <- syncProgress{loaded: loaded, total: total}:
				default:
				}
			}

			result, err := svc.SyncLibrary(ctx, lib, onProgress)

			doneCh <- syncProgress{
				loaded:    result.Count,
				total:     result.Count,
				done:      true,
				fromCache: result.FromCache,
				err:       err,
			}
		}()

		return readSyncProgress(lib, generation, progressCh, doneCh)
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

// readSyncProgress reads the next progress or terminal message and converts
// it to a LibrarySyncProgressMsg. The terminal message comes from the
// dedicated done channel, which is buffered and therefore never lost.
func readSyncProgress(lib domain.Library, generation int, progressCh <-chan syncProgress, doneCh <-chan syncProgress) tea.Msg {
	makeMsg := func(p syncProgress) LibrarySyncProgressMsg {
		return LibrarySyncProgressMsg{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Generation:  generation,
			Loaded:      p.loaded,
			Total:       p.total,
			Done:        p.done,
			FromCache:   p.fromCache,
			Error:       p.err,
		}
	}

	select {
	case p := <-doneCh:
		return makeMsg(p)
	case p, ok := <-progressCh:
		if !ok {
			// Progress channel closed: the terminal message is guaranteed to
			// already be in doneCh (sent before the deferred close runs)
			return makeMsg(<-doneCh)
		}
		msg := makeMsg(p)
		if !p.done && p.err == nil {
			msg.NextCmd = listenToSyncCmd(lib, generation, progressCh, doneCh)
		}
		return msg
	}
}

// listenToSyncCmd returns a command that reads the next message from the progress channel
func listenToSyncCmd(lib domain.Library, generation int, progressCh <-chan syncProgress, doneCh <-chan syncProgress) tea.Cmd {
	return func() tea.Msg {
		return readSyncProgress(lib, generation, progressCh, doneCh)
	}
}

// SyncAllLibrariesCmd syncs all libraries in parallel
func SyncAllLibrariesCmd(svc *library.Service, libraries []domain.Library, generation int) tea.Cmd {
	teaCmds := make([]tea.Cmd, len(libraries))
	for i, lib := range libraries {
		teaCmds[i] = SyncLibraryCmd(svc, lib, generation)
	}
	return tea.Batch(teaCmds...)
}

// SyncPlaylistsCmd syncs playlists and their items (two levels deep, like library sync).
func SyncPlaylistsCmd(svc *playlist.Service, playlistsID string, generation int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// SyncPlaylists fetches playlists AND items for each
		playlists, err := svc.SyncPlaylists(ctx)

		return LibrarySyncProgressMsg{
			LibraryID:   playlistsID,
			LibraryType: "playlist",
			Generation:  generation,
			Loaded:      len(playlists),
			Total:       len(playlists),
			Done:        true,
			Error:       err,
		}
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
func LoadPlaylistsCmd(svc *playlist.Service) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlists, err := svc.FetchPlaylists(ctx)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlists"}
		}
		return PlaylistsLoadedMsg{Playlists: playlists}
	}
}

// LoadPlaylistItemsCmd loads items from a playlist
func LoadPlaylistItemsCmd(svc *playlist.Service, playlistID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		items, err := svc.FetchPlaylistItems(ctx, playlistID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading playlist items"}
		}
		return PlaylistItemsLoadedMsg{Items: items, PlaylistID: playlistID}
	}
}

// CreatePlaylistCmd creates a new playlist
func CreatePlaylistCmd(svc *playlist.Service, title string, itemIDs []string) tea.Cmd {
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
func AddToPlaylistCmd(svc *playlist.Service, playlistID string, itemIDs []string) tea.Cmd {
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
func RemoveFromPlaylistCmd(svc *playlist.Service, playlistID, itemID string) tea.Cmd {
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
func DeletePlaylistCmd(svc *playlist.Service, playlistID string) tea.Cmd {
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
func LoadPlaylistModalDataCmd(svc *playlist.Service, item *domain.MediaItem) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		playlists, err := svc.FetchPlaylists(ctx)
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
