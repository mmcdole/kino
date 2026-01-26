package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/adapter"
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

// LoadLibraryContentCmd loads content (movies AND shows) from a mixed library
func LoadLibraryContentCmd(svc *service.LibraryService, libID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		items, err := svc.GetLibraryContent(ctx, libID)
		if err != nil {
			return ErrMsg{Err: err, Context: "loading library content"}
		}
		return LibraryContentLoadedMsg{Items: items, LibraryID: libID}
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
		return MarkWatchedMsg{ItemID: itemID, Title: title}
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

// LoadAllForGlobalSearchCmd loads cached content from all libraries for global search
// Uses cache-only access to avoid blocking network requests and UI freezes
func LoadAllForGlobalSearchCmd(libSvc *service.LibraryService, searchSvc *service.SearchService, libraries []domain.Library) tea.Cmd {
	return func() tea.Msg {
		var movieCount, showCount, episodeCount int
		var skippedLibraries int

		for _, lib := range libraries {
			switch lib.Type {
			case "movie":
				movies := libSvc.GetCachedMovies(lib.ID)
				if movies == nil {
					skippedLibraries++
					continue // Skip - not cached yet
				}
				// Index movies (already pointers)
				items := make([]service.FilterItem, len(movies))
				for i, movie := range movies {
					items[i] = service.FilterItem{
						Item:  movie,
						Title: movie.Title,
						Type:  domain.MediaTypeMovie,
						NavContext: service.NavigationContext{
							LibraryID:   lib.ID,
							LibraryName: lib.Name,
							MovieID:     movie.ID,
						},
					}
				}
				searchSvc.IndexForFilter(items)
				movieCount += len(movies)

			case "show":
				shows := libSvc.GetCachedShows(lib.ID)
				if shows == nil {
					skippedLibraries++
					continue // Skip - not cached yet
				}
				// Index shows (already pointers)
				items := make([]service.FilterItem, len(shows))
				for i, show := range shows {
					items[i] = service.FilterItem{
						Item:  show,
						Title: show.Title,
						Type:  domain.MediaTypeShow,
						NavContext: service.NavigationContext{
							LibraryID:   lib.ID,
							LibraryName: lib.Name,
							ShowID:      show.ID,
							ShowTitle:   show.Title,
						},
					}
				}
				searchSvc.IndexForFilter(items)
				showCount += len(shows)

			case "mixed":
				content := libSvc.GetCachedLibraryContent(lib.ID)
				if content == nil {
					skippedLibraries++
					continue // Skip - not cached yet
				}
				// Index mixed content
				items := make([]service.FilterItem, 0, len(content))
				for _, item := range content {
					switch v := item.(type) {
					case *domain.MediaItem:
						items = append(items, service.FilterItem{
							Item:  v,
							Title: v.Title,
							Type:  domain.MediaTypeMovie,
							NavContext: service.NavigationContext{
								LibraryID:   lib.ID,
								LibraryName: lib.Name,
								MovieID:     v.ID,
							},
						})
						movieCount++
					case *domain.Show:
						items = append(items, service.FilterItem{
							Item:  v,
							Title: v.Title,
							Type:  domain.MediaTypeShow,
							NavContext: service.NavigationContext{
								LibraryID:   lib.ID,
								LibraryName: lib.Name,
								ShowID:      v.ID,
								ShowTitle:   v.Title,
							},
						})
						showCount++
					}
				}
				searchSvc.IndexForFilter(items)
			}
		}

		return GlobalSearchReadyMsg{
			MovieCount:       movieCount,
			ShowCount:        showCount,
			EpisodeCount:     episodeCount,
			SkippedLibraries: skippedLibraries,
		}
	}
}

// SyncLibraryCmd performs smart sync with streaming progress updates using channels
// Uses a continuation pattern to pump all progress messages to the UI
func SyncLibraryCmd(
	libSvc *service.LibraryService,
	searchSvc *service.SearchService,
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
			libSvc.SmartSync(ctx, lib, force, progressCh)
		}()

		// Read the first message and return it with continuation context
		return readSyncProgress(lib, progressCh, searchSvc)
	}
}

// readSyncProgress reads one message from the channel and creates a LibrarySyncProgressMsg
// with the continuation command embedded
func readSyncProgress(
	lib domain.Library,
	progressCh <-chan service.SyncProgress,
	searchSvc *service.SearchService,
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

	// Index chunk immediately for incremental search
	if progress.Items != nil {
		// Calculate offset: total loaded minus current chunk size
		var chunkSize int
		switch v := progress.Items.(type) {
		case []*domain.MediaItem:
			chunkSize = len(v)
		case []*domain.Show:
			chunkSize = len(v)
		}
		offset := progress.Loaded - chunkSize
		indexChunkForSearch(searchSvc, progress.Items, lib, offset)
	}

	msg := LibrarySyncProgressMsg{
		LibraryID:   progress.LibraryID,
		LibraryType: progress.LibraryType,
		Loaded:      progress.Loaded,
		Total:       progress.Total,
		Items:       progress.Items,
		Done:        progress.Done,
		FromDisk:    progress.FromDisk,
		Error:       progress.Error,
	}

	// If not done and no error, attach continuation command
	if !progress.Done && progress.Error == nil {
		msg.NextCmd = listenToSyncCmd(lib, progressCh, searchSvc)
	}

	return msg
}

// listenToSyncCmd returns a command that reads the next message from the progress channel
func listenToSyncCmd(
	lib domain.Library,
	progressCh <-chan service.SyncProgress,
	searchSvc *service.SearchService,
) tea.Cmd {
	return func() tea.Msg {
		return readSyncProgress(lib, progressCh, searchSvc)
	}
}

// SyncAllLibrariesCmd syncs all libraries in parallel
func SyncAllLibrariesCmd(
	libSvc *service.LibraryService,
	searchSvc *service.SearchService,
	libraries []domain.Library,
	force bool,
) tea.Cmd {
	cmds := make([]tea.Cmd, len(libraries))
	for i, lib := range libraries {
		cmds[i] = SyncLibraryCmd(libSvc, searchSvc, lib, force)
	}
	return tea.Batch(cmds...)
}

// indexChunkForSearch indexes a chunk of items for global search
// offset is the starting index of this chunk in the full library list
func indexChunkForSearch(searchSvc *service.SearchService, items interface{}, lib domain.Library, offset int) {
	switch v := items.(type) {
	case []*domain.MediaItem:
		filterItems := make([]service.FilterItem, len(v))
		for i, movie := range v {
			filterItems[i] = service.FilterItem{
				Item:  movie,
				Title: movie.Title,
				Type:  domain.MediaTypeMovie,
				NavContext: service.NavigationContext{
					LibraryID:   lib.ID,
					LibraryName: lib.Name,
					MovieID:     movie.ID,
				},
			}
		}
		searchSvc.IndexForFilter(filterItems)

	case []*domain.Show:
		filterItems := make([]service.FilterItem, len(v))
		for i, show := range v {
			filterItems[i] = service.FilterItem{
				Item:  show,
				Title: show.Title,
				Type:  domain.MediaTypeShow,
				NavContext: service.NavigationContext{
					LibraryID:   lib.ID,
					LibraryName: lib.Name,
					ShowID:      show.ID,
					ShowTitle:   show.Title,
				},
			}
		}
		searchSvc.IndexForFilter(filterItems)

	case []domain.ListItem:
		filterItems := make([]service.FilterItem, 0, len(v))
		for _, item := range v {
			switch t := item.(type) {
			case *domain.MediaItem:
				filterItems = append(filterItems, service.FilterItem{
					Item:  t,
					Title: t.Title,
					Type:  domain.MediaTypeMovie,
					NavContext: service.NavigationContext{
						LibraryID:   lib.ID,
						LibraryName: lib.Name,
						MovieID:     t.ID,
					},
				})
			case *domain.Show:
				filterItems = append(filterItems, service.FilterItem{
					Item:  t,
					Title: t.Title,
					Type:  domain.MediaTypeShow,
					NavContext: service.NavigationContext{
						LibraryID:   lib.ID,
						LibraryName: lib.Name,
						ShowID:      t.ID,
						ShowTitle:   t.Title,
					},
				})
			}
		}
		searchSvc.IndexForFilter(filterItems)
	}
}

// LogoutCmd clears server config and cache, then signals completion
func LogoutCmd() tea.Cmd {
	return func() tea.Msg {
		// Clear server configuration
		if err := adapter.ClearServerConfig(); err != nil {
			return LogoutCompleteMsg{Error: err}
		}

		// Clear cache
		if err := adapter.ClearCache(); err != nil {
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
			return PlaylistUpdatedMsg{PlaylistID: playlistID, Success: false, Error: err}
		}
		return PlaylistUpdatedMsg{PlaylistID: playlistID, Success: true}
	}
}

// RemoveFromPlaylistCmd removes an item from a playlist
func RemoveFromPlaylistCmd(svc *service.PlaylistService, playlistID, itemID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := svc.RemoveFromPlaylist(ctx, playlistID, itemID)
		if err != nil {
			return PlaylistUpdatedMsg{PlaylistID: playlistID, Success: false, Error: err}
		}
		return PlaylistUpdatedMsg{PlaylistID: playlistID, Success: true}
	}
}

// DeletePlaylistCmd deletes a playlist
func DeletePlaylistCmd(svc *service.PlaylistService, playlistID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := svc.DeletePlaylist(ctx, playlistID)
		if err != nil {
			return PlaylistDeletedMsg{PlaylistID: playlistID, Success: false, Error: err}
		}
		return PlaylistDeletedMsg{PlaylistID: playlistID, Success: true}
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
