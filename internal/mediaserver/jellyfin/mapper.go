package jellyfin

import (
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

const (
	// Jellyfin uses 100-nanosecond ticks
	ticksPerMillisecond = 10000
	ticksPerSecond      = 10000000
)

// MapLibraries converts Jellyfin virtual folders to domain libraries
func MapLibraries(items []Item) []domain.Library {
	libraries := make([]domain.Library, 0, len(items))
	for _, item := range items {
		lib := mapLibrary(item)
		if lib != nil {
			libraries = append(libraries, *lib)
		}
	}
	return libraries
}

// mapLibrary converts a single Jellyfin item to a domain library
func mapLibrary(item Item) *domain.Library {
	// Map Jellyfin CollectionType to our library type
	var libType string
	switch item.CollectionType {
	case "movies":
		libType = "movie"
	case "tvshows":
		libType = "show"
	case "mixed", "":
		// Mixed libraries contain both movies and shows
		libType = "mixed"
	default:
		// Skip other library types (music, etc.)
		return nil
	}

	// Parse the created date for UpdatedAt
	var updatedAt int64
	if item.DateCreated != "" {
		if t, err := time.Parse(time.RFC3339, item.DateCreated); err == nil {
			updatedAt = t.Unix()
		}
	}

	return &domain.Library{
		ID:        item.ID,
		Name:      item.Name,
		Type:      libType,
		UpdatedAt: updatedAt,
	}
}

// MapMovies converts Jellyfin items to domain media items (movies)
func MapMovies(items []Item, serverURL string) []*domain.MediaItem {
	movies := make([]*domain.MediaItem, 0, len(items))
	for _, item := range items {
		if item.Type != "Movie" {
			continue
		}
		movie := mapMovie(item, serverURL)
		movies = append(movies, &movie)
	}
	return movies
}

// mapMovie converts a single Jellyfin movie item to a domain media item
func mapMovie(item Item, serverURL string) domain.MediaItem {
	mi := domain.MediaItem{
		ID:        item.ID,
		Title:     item.Name,
		SortTitle: item.SortName,
		LibraryID: item.ParentID,
		Summary:   item.Overview,
		Year:      item.ProductionYear,
		Duration:  ticksToDuration(item.RunTimeTicks),
		Format:    extractVideoCodec(item),
		Type:      domain.MediaTypeMovie,
	}

	if mi.SortTitle == "" {
		mi.SortTitle = mi.Title
	}

	// Parse dates
	if item.DateCreated != "" {
		if t, err := time.Parse(time.RFC3339, item.DateCreated); err == nil {
			mi.AddedAt = t.Unix()
			mi.UpdatedAt = t.Unix() // For movies, UpdatedAt = AddedAt
		}
	}

	// User data (watch status, progress)
	if item.UserData != nil {
		mi.IsPlayed = item.UserData.Played
		mi.ViewOffset = ticksToDuration(item.UserData.PlaybackPositionTicks)
	}

	return mi
}

// MapShows converts Jellyfin items to domain shows
func MapShows(items []Item, serverURL string) []*domain.Show {
	shows := make([]*domain.Show, 0, len(items))
	for _, item := range items {
		if item.Type != "Series" {
			continue
		}
		show := mapShow(item, serverURL)
		shows = append(shows, &show)
	}
	return shows
}

// mapShow converts a single Jellyfin series item to a domain show
func mapShow(item Item, serverURL string) domain.Show {
	show := domain.Show{
		ID:           item.ID,
		Title:        item.Name,
		SortTitle:    item.SortName,
		LibraryID:    item.ParentID,
		Summary:      item.Overview,
		Year:         item.ProductionYear,
		SeasonCount:  item.ChildCount,
		EpisodeCount: item.RecursiveItemCount,
	}

	if show.SortTitle == "" {
		show.SortTitle = show.Title
	}

	// Parse dates
	if item.DateCreated != "" {
		if t, err := time.Parse(time.RFC3339, item.DateCreated); err == nil {
			show.AddedAt = t.Unix()
		}
	}

	// UpdatedAt: prefer DateLastMediaAdded (when last episode was added), fallback to DateCreated
	if item.DateLastMediaAdded != "" {
		if t, err := time.Parse(time.RFC3339, item.DateLastMediaAdded); err == nil {
			show.UpdatedAt = t.Unix()
		}
	} else if item.DateCreated != "" {
		if t, err := time.Parse(time.RFC3339, item.DateCreated); err == nil {
			show.UpdatedAt = t.Unix()
		}
	}

	// User data (unwatched count)
	if item.UserData != nil {
		show.UnwatchedCount = item.UserData.UnplayedItemCount
	}

	return show
}

// MapSeasons converts Jellyfin items to domain seasons
func MapSeasons(items []Item, serverURL string) []*domain.Season {
	seasons := make([]*domain.Season, 0, len(items))
	for _, item := range items {
		if item.Type != "Season" {
			continue
		}
		season := mapSeason(item, serverURL)
		seasons = append(seasons, &season)
	}
	return seasons
}

// mapSeason converts a single Jellyfin season item to a domain season
func mapSeason(item Item, serverURL string) domain.Season {
	season := domain.Season{
		ID:           item.ID,
		ShowID:       item.SeriesID,
		ShowTitle:    item.SeriesName,
		SeasonNum:    item.IndexNumber, // Jellyfin uses IndexNumber for season number
		Title:        item.Name,
		EpisodeCount: item.ChildCount,
	}

	// User data (unwatched count)
	if item.UserData != nil {
		season.UnwatchedCount = item.UserData.UnplayedItemCount
	}

	return season
}

// MapEpisodes converts Jellyfin items to domain media items (episodes)
func MapEpisodes(items []Item, serverURL string) []*domain.MediaItem {
	episodes := make([]*domain.MediaItem, 0, len(items))
	for _, item := range items {
		if item.Type != "Episode" {
			continue
		}
		episode := mapEpisode(item, serverURL)
		episodes = append(episodes, &episode)
	}
	return episodes
}

// mapEpisode converts a single Jellyfin episode item to a domain media item
func mapEpisode(item Item, serverURL string) domain.MediaItem {
	mi := domain.MediaItem{
		ID:         item.ID,
		Title:      item.Name,
		SortTitle:  item.SortName,
		Summary:    item.Overview,
		Year:       item.ProductionYear,
		Duration:   ticksToDuration(item.RunTimeTicks),
		Format:     extractVideoCodec(item),
		Type:       domain.MediaTypeEpisode,
		ShowTitle:  item.SeriesName,
		ShowID:     item.SeriesID,
		SeasonNum:  item.ParentIndexNumber,
		EpisodeNum: item.IndexNumber,
		ParentID:   item.SeasonID,
	}

	if mi.SortTitle == "" {
		mi.SortTitle = mi.Title
	}

	// Parse dates
	if item.DateCreated != "" {
		if t, err := time.Parse(time.RFC3339, item.DateCreated); err == nil {
			mi.AddedAt = t.Unix()
			mi.UpdatedAt = t.Unix() // For episodes, UpdatedAt = AddedAt
		}
	}

	// User data (watch status, progress)
	if item.UserData != nil {
		mi.IsPlayed = item.UserData.Played
		mi.ViewOffset = ticksToDuration(item.UserData.PlaybackPositionTicks)
	}

	return mi
}

// MapSearchResults converts Jellyfin search hints to domain media items
func MapSearchResults(hints []SearchHint, serverURL string) []*domain.MediaItem {
	items := make([]*domain.MediaItem, 0, len(hints))
	for _, hint := range hints {
		item := mapSearchHint(hint, serverURL)
		if item != nil {
			items = append(items, item)
		}
	}
	return items
}

// mapSearchHint converts a Jellyfin search hint to a domain media item
func mapSearchHint(hint SearchHint, serverURL string) *domain.MediaItem {
	var mediaType domain.MediaType
	switch hint.Type {
	case "Movie":
		mediaType = domain.MediaTypeMovie
	case "Episode":
		mediaType = domain.MediaTypeEpisode
	case "Series":
		mediaType = domain.MediaTypeShow
	default:
		return nil // Skip non-video types
	}

	item := &domain.MediaItem{
		ID:         hint.ID,
		Title:      hint.Name,
		Year:       hint.ProductionYear,
		Duration:   ticksToDuration(hint.RunTimeTicks),
		Type:       mediaType,
		ShowTitle:  hint.SeriesName,
		SeasonNum:  hint.ParentIndexNumber,
		EpisodeNum: hint.IndexNumber,
	}

	return item
}

// ticksToDuration converts Jellyfin 100-nanosecond ticks to time.Duration
func ticksToDuration(ticks int64) time.Duration {
	return time.Duration(ticks * 100) // 100ns per tick
}

// extractVideoCodec extracts the video codec from item media streams
func extractVideoCodec(item Item) string {
	// Try MediaSources first
	for _, source := range item.MediaSources {
		for _, stream := range source.MediaStreams {
			if stream.Type == "Video" {
				return normalizeCodec(stream.Codec)
			}
		}
	}

	// Fall back to direct MediaStreams
	for _, stream := range item.MediaStreams {
		if stream.Type == "Video" {
			return normalizeCodec(stream.Codec)
		}
	}

	return ""
}

// normalizeCodec converts codec names to display format
func normalizeCodec(codec string) string {
	switch strings.ToLower(codec) {
	case "hevc", "h265":
		return "HEVC"
	case "h264", "avc":
		return "H.264"
	case "mpeg4":
		return "MPEG4"
	case "vc1":
		return "VC-1"
	case "vp9":
		return "VP9"
	case "av1":
		return "AV1"
	default:
		return strings.ToUpper(codec)
	}
}

// MapPlaylists converts Jellyfin items to domain playlists
func MapPlaylists(items []Item, serverURL string) []*domain.Playlist {
	playlists := make([]*domain.Playlist, 0, len(items))
	for _, item := range items {
		if item.Type != "Playlist" {
			continue
		}
		playlist := mapPlaylist(item, serverURL)
		playlists = append(playlists, &playlist)
	}
	return playlists
}

// mapPlaylist converts a single Jellyfin playlist item to a domain playlist
func mapPlaylist(item Item, serverURL string) domain.Playlist {
	p := domain.Playlist{
		ID:           item.ID,
		Title:        item.Name,
		PlaylistType: "video", // Jellyfin playlists don't have a type field; default to video
		Smart:        false,   // Jellyfin smart playlists would need different detection
		ItemCount:    item.ChildCount,
		Duration:     ticksToDuration(item.RunTimeTicks),
	}

	// Parse dates
	if item.DateCreated != "" {
		if t, err := time.Parse(time.RFC3339, item.DateCreated); err == nil {
			p.UpdatedAt = t.Unix()
		}
	}

	return p
}

// MapLibraryContent converts Jellyfin items to domain.ListItem for mixed libraries.
// This handles both Movies and Series in a single response, returning them as
// a polymorphic slice that the UI can display uniformly.
func MapLibraryContent(items []Item, serverURL string) []domain.ListItem {
	result := make([]domain.ListItem, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "Movie":
			movie := mapMovie(item, serverURL)
			result = append(result, &movie)
		case "Series":
			show := mapShow(item, serverURL)
			result = append(result, &show)
		}
	}
	return result
}
