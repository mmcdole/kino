package jellyfin

import (
	"fmt"
	"time"

	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/mediaserver/normalize"
)

const (
	// Jellyfin uses 100-nanosecond ticks
	ticksPerMillisecond = 10000
	ticksPerSecond      = 10000000
)

// mapLibraries converts Jellyfin virtual folders to media libraries
func mapLibraries(items []baseItem) []media.Library {
	libraries := make([]media.Library, 0, len(items))
	for _, item := range items {
		lib := mapLibrary(item)
		if lib != nil {
			libraries = append(libraries, *lib)
		}
	}
	return libraries
}

// mapLibrary converts a single Jellyfin item to a media library
func mapLibrary(item baseItem) *media.Library {
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

	return &media.Library{
		ID:        item.ID,
		Name:      item.Name,
		Type:      libType,
		UpdatedAt: updatedAt,
	}
}

// mapMovies converts Jellyfin items to media items (movies)
func mapMovies(items []baseItem, serverURL string) []*media.Item {
	movies := make([]*media.Item, 0, len(items))
	for _, item := range items {
		if item.Type != "Movie" {
			continue
		}
		movie := mapMovie(item, serverURL)
		movies = append(movies, &movie)
	}
	return movies
}

// mapMovie converts a single Jellyfin movie item to a media item
func mapMovie(item baseItem, serverURL string) media.Item {
	mi := media.Item{
		ID:        item.ID,
		Title:     item.Name,
		SortTitle: item.SortName,
		LibraryID: item.ParentID,
		Summary:   item.Overview,
		Year:      item.ProductionYear,
		Duration:  ticksToDuration(item.RunTimeTicks),
		Type:      media.TypeMovie,
	}

	if mi.SortTitle == "" {
		mi.SortTitle = mi.Title
	}

	mi.Rating = item.CommunityRating

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

	// Image URLs
	if item.ImageTags.Primary != "" {
		mi.ThumbURL = fmt.Sprintf("%s/Items/%s/Images/Primary?tag=%s", serverURL, item.ID, item.ImageTags.Primary)
	}

	mi.ContentRating = normalize.ContentRating(item.OfficialRating)
	mi.VideoCodec = extractVideoCodec(item)
	mi.AudioCodec, mi.AudioChannels = extractAudioInfo(item)
	mi.Container = normalize.Container(item.Container)
	if len(item.MediaSources) > 0 {
		src := item.MediaSources[0]
		mi.FileSize = src.Size
		mi.Bitrate = extractBitrate(item)
		mi.Width, mi.Height = extractResolution(item)
	}

	return mi
}

// mapShows converts Jellyfin items to media shows
func mapShows(items []baseItem, serverURL string) []*media.Show {
	shows := make([]*media.Show, 0, len(items))
	for _, item := range items {
		if item.Type != "Series" {
			continue
		}
		show := mapShow(item, serverURL)
		shows = append(shows, &show)
	}
	return shows
}

// mapShow converts a single Jellyfin series item to a media show
func mapShow(item baseItem, serverURL string) media.Show {
	show := media.Show{
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

	show.Rating = item.CommunityRating
	show.ContentRating = normalize.ContentRating(item.OfficialRating)

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

	// Image URLs
	if item.ImageTags.Primary != "" {
		show.ThumbURL = fmt.Sprintf("%s/Items/%s/Images/Primary?tag=%s", serverURL, item.ID, item.ImageTags.Primary)
	}

	return show
}

// mapSeasons converts Jellyfin items to media seasons
func mapSeasons(items []baseItem, serverURL string) []*media.Season {
	seasons := make([]*media.Season, 0, len(items))
	for _, item := range items {
		if item.Type != "Season" {
			continue
		}
		season := mapSeason(item, serverURL)
		seasons = append(seasons, &season)
	}
	return seasons
}

// mapSeason converts a single Jellyfin season item to a media season
func mapSeason(item baseItem, serverURL string) media.Season {
	season := media.Season{
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

	// Image URLs
	if item.ImageTags.Primary != "" {
		season.ThumbURL = fmt.Sprintf("%s/Items/%s/Images/Primary?tag=%s", serverURL, item.ID, item.ImageTags.Primary)
	}

	return season
}

// mapEpisodes converts Jellyfin items to media items (episodes)
func mapEpisodes(items []baseItem, serverURL string) []*media.Item {
	episodes := make([]*media.Item, 0, len(items))
	for _, item := range items {
		if item.Type != "Episode" {
			continue
		}
		episode := mapEpisode(item, serverURL)
		episodes = append(episodes, &episode)
	}
	return episodes
}

// mapEpisode converts a single Jellyfin episode item to a media item
func mapEpisode(item baseItem, serverURL string) media.Item {
	mi := media.Item{
		ID:         item.ID,
		Title:      item.Name,
		SortTitle:  item.SortName,
		Summary:    item.Overview,
		Year:       item.ProductionYear,
		Duration:   ticksToDuration(item.RunTimeTicks),
		Type:       media.TypeEpisode,
		ShowTitle:  item.SeriesName,
		ShowID:     item.SeriesID,
		SeasonNum:  item.ParentIndexNumber,
		EpisodeNum: item.IndexNumber,
		ParentID:   item.SeasonID,
	}

	if mi.SortTitle == "" {
		mi.SortTitle = mi.Title
	}

	mi.Rating = item.CommunityRating

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

	// Image URLs
	if item.ImageTags.Primary != "" {
		mi.ThumbURL = fmt.Sprintf("%s/Items/%s/Images/Primary?tag=%s", serverURL, item.ID, item.ImageTags.Primary)
	}

	mi.ContentRating = normalize.ContentRating(item.OfficialRating)
	mi.VideoCodec = extractVideoCodec(item)
	mi.AudioCodec, mi.AudioChannels = extractAudioInfo(item)
	mi.Container = normalize.Container(item.Container)
	if len(item.MediaSources) > 0 {
		src := item.MediaSources[0]
		mi.FileSize = src.Size
		mi.Bitrate = extractBitrate(item)
		mi.Width, mi.Height = extractResolution(item)
	}

	return mi
}

// ticksToDuration converts Jellyfin 100-nanosecond ticks to time.Duration
func ticksToDuration(ticks int64) time.Duration {
	return time.Duration(ticks * 100) // 100ns per tick
}

// extractVideoCodec extracts the video codec from item media streams
func extractVideoCodec(item baseItem) string {
	// Try MediaSources first
	for _, source := range item.MediaSources {
		for _, stream := range source.MediaStreams {
			if stream.Type == "Video" {
				return normalize.VideoCodec(stream.Codec)
			}
		}
	}

	// Fall back to direct MediaStreams
	for _, stream := range item.MediaStreams {
		if stream.Type == "Video" {
			return normalize.VideoCodec(stream.Codec)
		}
	}

	return ""
}

// extractAudioInfo extracts the audio codec and channel count from item media streams
func extractAudioInfo(item baseItem) (string, int) {
	for _, source := range item.MediaSources {
		for _, stream := range source.MediaStreams {
			if stream.Type == "Audio" {
				return normalize.AudioCodec(stream.Codec), stream.Channels
			}
		}
	}
	for _, stream := range item.MediaStreams {
		if stream.Type == "Audio" {
			return normalize.AudioCodec(stream.Codec), stream.Channels
		}
	}
	return "", 0
}

// extractBitrate extracts the video bitrate from item media streams
func extractBitrate(item baseItem) int {
	for _, source := range item.MediaSources {
		for _, stream := range source.MediaStreams {
			if stream.Type == "Video" && stream.BitRate > 0 {
				return stream.BitRate / 1000 // Convert to kbps
			}
		}
	}
	for _, stream := range item.MediaStreams {
		if stream.Type == "Video" && stream.BitRate > 0 {
			return stream.BitRate / 1000
		}
	}
	return 0
}

// extractResolution extracts the video width and height from item media streams
func extractResolution(item baseItem) (int, int) {
	for _, source := range item.MediaSources {
		for _, stream := range source.MediaStreams {
			if stream.Type == "Video" && stream.Width > 0 {
				return stream.Width, stream.Height
			}
		}
	}
	for _, stream := range item.MediaStreams {
		if stream.Type == "Video" && stream.Width > 0 {
			return stream.Width, stream.Height
		}
	}
	return 0, 0
}

// mapPlaylists converts Jellyfin items to media playlists
func mapPlaylists(items []baseItem, serverURL string) []*media.Playlist {
	playlists := make([]*media.Playlist, 0, len(items))
	for _, item := range items {
		if item.Type != "Playlist" {
			continue
		}
		playlist := mapPlaylist(item, serverURL)
		playlists = append(playlists, &playlist)
	}
	return playlists
}

// mapPlaylist converts a single Jellyfin playlist item to a media playlist
func mapPlaylist(item baseItem, serverURL string) media.Playlist {
	p := media.Playlist{
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

// mapLibraryContent converts Jellyfin items to media.ListItem for mixed libraries.
// This handles both Movies and Series in a single response, returning them as
// a polymorphic slice that the UI can display uniformly.
func mapLibraryContent(items []baseItem, serverURL string) []media.ListItem {
	result := make([]media.ListItem, 0, len(items))
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
