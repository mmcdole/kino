package plex

import (
	"strconv"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver/normalize"
)

// MapLibraries converts Plex directories to domain libraries
func MapLibraries(dirs []Directory) []domain.Library {
	libraries := make([]domain.Library, 0, len(dirs))
	for _, d := range dirs {
		// Only include movie and show libraries
		if d.Type != "movie" && d.Type != "show" {
			continue
		}
		libraries = append(libraries, domain.Library{
			ID:        d.Key,
			Name:      d.Title,
			Type:      d.Type,
			UpdatedAt: d.ContentChangedAt,
		})
	}
	return libraries
}

// MapMovies converts Plex metadata to domain media items (movies)
func MapMovies(metadata []Metadata, serverURL string) []*domain.MediaItem {
	items := make([]*domain.MediaItem, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "movie" {
			continue
		}
		item := mapMovie(m, serverURL)
		items = append(items, &item)
	}
	return items
}

// mapMovie converts a single movie metadata to domain media item
func mapMovie(m Metadata, serverURL string) domain.MediaItem {
	item := domain.MediaItem{
		ID:         m.RatingKey,
		Title:      m.Title,
		SortTitle:  m.TitleSort,
		LibraryID:  strconv.Itoa(m.LibrarySectionID),
		Summary:    m.Summary,
		Year:       m.Year,
		AddedAt:    m.AddedAt,
		UpdatedAt:  m.UpdatedAt,
		Duration:   time.Duration(m.Duration) * time.Millisecond,
		ViewOffset: time.Duration(m.ViewOffset) * time.Millisecond,
		IsPlayed:   m.ViewCount > 0,
		Type:       domain.MediaTypeMovie,
	}

	if item.SortTitle == "" {
		item.SortTitle = item.Title
	}

	if m.AudienceRating > 0 {
		item.Rating = m.AudienceRating
	} else if m.Rating > 0 {
		item.Rating = m.Rating
	}

	if m.Thumb != "" {
		item.ThumbURL = serverURL + m.Thumb
	}
	if m.Art != "" {
		item.ArtURL = serverURL + m.Art
	}

	item.ContentRating = normalize.ContentRating(m.ContentRating)
	if len(m.Media) > 0 {
		media := m.Media[0]
		item.Bitrate = media.Bitrate
		item.Width = media.Width
		item.Height = media.Height
		item.VideoCodec = normalize.VideoCodec(media.VideoCodec)
		item.AudioCodec = normalize.AudioCodec(media.AudioCodec)
		item.AudioChannels = media.AudioChannels
		item.Container = normalize.Container(media.Container)
		if len(media.Part) > 0 {
			item.FileSize = media.Part[0].Size
		}
	}

	return item
}

// MapShows converts Plex metadata to domain shows
func MapShows(metadata []Metadata, serverURL string) []*domain.Show {
	shows := make([]*domain.Show, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "show" {
			continue
		}
		show := mapShow(m, serverURL)
		shows = append(shows, &show)
	}
	return shows
}

// mapShow converts a single show metadata to domain show
func mapShow(m Metadata, serverURL string) domain.Show {
	show := domain.Show{
		ID:             m.RatingKey,
		Title:          m.Title,
		SortTitle:      m.TitleSort,
		LibraryID:      strconv.Itoa(m.LibrarySectionID),
		Summary:        m.Summary,
		Year:           m.Year,
		AddedAt:        m.AddedAt,
		UpdatedAt:      m.UpdatedAt,
		SeasonCount:    m.ChildCount,
		EpisodeCount:   m.LeafCount,
		UnwatchedCount: m.LeafCount - m.ViewedLeafCount,
	}

	if show.SortTitle == "" {
		show.SortTitle = show.Title
	}

	if m.AudienceRating > 0 {
		show.Rating = m.AudienceRating
	} else if m.Rating > 0 {
		show.Rating = m.Rating
	}

	if m.Thumb != "" {
		show.ThumbURL = serverURL + m.Thumb
	}
	if m.Art != "" {
		show.ArtURL = serverURL + m.Art
	}

	show.ContentRating = normalize.ContentRating(m.ContentRating)

	return show
}

// MapSeasons converts Plex metadata to domain seasons
func MapSeasons(metadata []Metadata, serverURL string) []*domain.Season {
	seasons := make([]*domain.Season, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "season" {
			continue
		}
		season := mapSeason(m, serverURL)
		seasons = append(seasons, &season)
	}
	return seasons
}

// mapSeason converts a single season metadata to domain season
func mapSeason(m Metadata, serverURL string) domain.Season {
	season := domain.Season{
		ID:             m.RatingKey,
		ShowID:         m.ParentRatingKey,
		ShowTitle:      m.ParentTitle,
		SeasonNum:      m.Index,
		Title:          m.Title,
		EpisodeCount:   m.LeafCount,
		UnwatchedCount: m.LeafCount - m.ViewedLeafCount,
	}

	if m.Thumb != "" {
		season.ThumbURL = serverURL + m.Thumb
	}

	return season
}

// MapEpisodes converts Plex metadata to domain media items (episodes)
func MapEpisodes(metadata []Metadata, serverURL string) []*domain.MediaItem {
	items := make([]*domain.MediaItem, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "episode" {
			continue
		}
		item := mapEpisode(m, serverURL)
		items = append(items, &item)
	}
	return items
}

// mapEpisode converts a single episode metadata to domain media item
func mapEpisode(m Metadata, serverURL string) domain.MediaItem {
	item := domain.MediaItem{
		ID:         m.RatingKey,
		Title:      m.Title,
		SortTitle:  m.TitleSort,
		LibraryID:  strconv.Itoa(m.LibrarySectionID),
		Summary:    m.Summary,
		Year:       m.Year,
		AddedAt:    m.AddedAt,
		UpdatedAt:  m.UpdatedAt,
		Duration:   time.Duration(m.Duration) * time.Millisecond,
		ViewOffset: time.Duration(m.ViewOffset) * time.Millisecond,
		IsPlayed:   m.ViewCount > 0,
		Type:       domain.MediaTypeEpisode,
		ShowTitle:  m.GrandparentTitle,
		ShowID:     m.GrandparentRatingKey,
		SeasonNum:  m.ParentIndex,
		EpisodeNum: m.Index,
		ParentID:   m.ParentRatingKey,
	}

	if item.SortTitle == "" {
		item.SortTitle = item.Title
	}

	if m.AudienceRating > 0 {
		item.Rating = m.AudienceRating
	} else if m.Rating > 0 {
		item.Rating = m.Rating
	}

	if m.Thumb != "" {
		item.ThumbURL = serverURL + m.Thumb
	}
	if m.Art != "" {
		item.ArtURL = serverURL + m.Art
	}

	item.ContentRating = normalize.ContentRating(m.ContentRating)
	if len(m.Media) > 0 {
		media := m.Media[0]
		item.Bitrate = media.Bitrate
		item.Width = media.Width
		item.Height = media.Height
		item.VideoCodec = normalize.VideoCodec(media.VideoCodec)
		item.AudioCodec = normalize.AudioCodec(media.AudioCodec)
		item.AudioChannels = media.AudioChannels
		item.Container = normalize.Container(media.Container)
		if len(media.Part) > 0 {
			item.FileSize = media.Part[0].Size
		}
	}

	return item
}

// MapVideoItems converts Plex metadata to playable domain media items
// (movies and episodes)
func MapVideoItems(metadata []Metadata, serverURL string) []*domain.MediaItem {
	items := make([]*domain.MediaItem, 0, len(metadata))
	for _, m := range metadata {
		switch m.Type {
		case "movie":
			item := mapMovie(m, serverURL)
			items = append(items, &item)
		case "episode":
			item := mapEpisode(m, serverURL)
			items = append(items, &item)
		}
	}
	return items
}

// MapPlaylists converts Plex metadata to domain playlists
func MapPlaylists(metadata []Metadata, serverURL string) []*domain.Playlist {
	playlists := make([]*domain.Playlist, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "playlist" {
			continue
		}
		playlist := mapPlaylist(m, serverURL)
		playlists = append(playlists, &playlist)
	}
	return playlists
}

// mapPlaylist converts a single Plex playlist metadata to domain playlist
func mapPlaylist(m Metadata, serverURL string) domain.Playlist {
	return domain.Playlist{
		ID:           m.RatingKey,
		Title:        m.Title,
		PlaylistType: "video",
		Smart:        false,
		ItemCount:    m.LeafCount,
		Duration:     time.Duration(m.Duration) * time.Millisecond,
		UpdatedAt:    m.UpdatedAt,
	}
}

// MapLibraryContent converts Plex metadata to domain.ListItem for mixed libraries.
// This handles both movies and shows in a single response, returning them as
// a polymorphic slice that the UI can display uniformly.
func MapLibraryContent(metadata []Metadata, serverURL string) []domain.ListItem {
	result := make([]domain.ListItem, 0, len(metadata))
	for _, m := range metadata {
		switch m.Type {
		case "movie":
			item := mapMovie(m, serverURL)
			result = append(result, &item)
		case "show":
			show := mapShow(m, serverURL)
			result = append(result, &show)
		}
	}
	return result
}
