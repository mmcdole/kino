package plex

import (
	"strconv"
	"time"

	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/mediaserver/normalize"
)

// mapLibraries converts Plex directories to media libraries
func mapLibraries(dirs []directory) []media.Library {
	libraries := make([]media.Library, 0, len(dirs))
	for _, d := range dirs {
		// Only include movie and show libraries
		if d.Type != "movie" && d.Type != "show" {
			continue
		}
		libraries = append(libraries, media.Library{
			ID:        d.Key,
			Name:      d.Title,
			Type:      d.Type,
			UpdatedAt: d.ContentChangedAt,
		})
	}
	return libraries
}

// mapMovies converts Plex metadata to media items (movies)
func mapMovies(metadata []metadataItem, serverURL string) []*media.Item {
	items := make([]*media.Item, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "movie" {
			continue
		}
		item := mapMovie(m, serverURL)
		items = append(items, &item)
	}
	return items
}

// mapMovie converts a single movie metadata to media item
func mapMovie(m metadataItem, serverURL string) media.Item {
	item := media.Item{
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
		Type:       media.TypeMovie,
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
		version := m.Media[0]
		item.Bitrate = version.Bitrate
		item.Width = version.Width
		item.Height = version.Height
		item.VideoCodec = normalize.VideoCodec(version.VideoCodec)
		item.AudioCodec = normalize.AudioCodec(version.AudioCodec)
		item.AudioChannels = version.AudioChannels
		item.Container = normalize.Container(version.Container)
		if len(version.Part) > 0 {
			item.FileSize = version.Part[0].Size
		}
	}

	return item
}

// mapShows converts Plex metadata to media shows
func mapShows(metadata []metadataItem, serverURL string) []*media.Show {
	shows := make([]*media.Show, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "show" {
			continue
		}
		show := mapShow(m, serverURL)
		shows = append(shows, &show)
	}
	return shows
}

// mapShow converts a single show metadata to media show
func mapShow(m metadataItem, serverURL string) media.Show {
	show := media.Show{
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

// mapSeasons converts Plex metadata to media seasons
func mapSeasons(metadata []metadataItem, serverURL string) []*media.Season {
	seasons := make([]*media.Season, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "season" {
			continue
		}
		season := mapSeason(m, serverURL)
		seasons = append(seasons, &season)
	}
	return seasons
}

// mapSeason converts a single season metadata to media season
func mapSeason(m metadataItem, serverURL string) media.Season {
	season := media.Season{
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

// mapEpisodes converts Plex metadata to media items (episodes)
func mapEpisodes(metadata []metadataItem, serverURL string) []*media.Item {
	items := make([]*media.Item, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "episode" {
			continue
		}
		item := mapEpisode(m, serverURL)
		items = append(items, &item)
	}
	return items
}

// mapEpisode converts a single episode metadata to media item
func mapEpisode(m metadataItem, serverURL string) media.Item {
	item := media.Item{
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
		Type:       media.TypeEpisode,
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
		version := m.Media[0]
		item.Bitrate = version.Bitrate
		item.Width = version.Width
		item.Height = version.Height
		item.VideoCodec = normalize.VideoCodec(version.VideoCodec)
		item.AudioCodec = normalize.AudioCodec(version.AudioCodec)
		item.AudioChannels = version.AudioChannels
		item.Container = normalize.Container(version.Container)
		if len(version.Part) > 0 {
			item.FileSize = version.Part[0].Size
		}
	}

	return item
}

// mapVideoItems converts Plex metadata to playable media items
// (movies and episodes)
func mapVideoItems(metadata []metadataItem, serverURL string) []*media.Item {
	items := make([]*media.Item, 0, len(metadata))
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

// mapPlaylists converts Plex metadata to media playlists
func mapPlaylists(metadata []metadataItem, serverURL string) []*media.Playlist {
	playlists := make([]*media.Playlist, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "playlist" {
			continue
		}
		playlist := mapPlaylist(m, serverURL)
		playlists = append(playlists, &playlist)
	}
	return playlists
}

// mapPlaylist converts a single Plex playlist metadata to media playlist
func mapPlaylist(m metadataItem, serverURL string) media.Playlist {
	return media.Playlist{
		ID:           m.RatingKey,
		Title:        m.Title,
		PlaylistType: "video",
		Smart:        false,
		ItemCount:    m.LeafCount,
		Duration:     time.Duration(m.Duration) * time.Millisecond,
		UpdatedAt:    m.UpdatedAt,
	}
}

// mapLibraryContent converts Plex metadata to media.ListItem for mixed libraries.
// This handles both movies and shows in a single response, returning them as
// a polymorphic slice that the UI can display uniformly.
func mapLibraryContent(metadata []metadataItem, serverURL string) []media.ListItem {
	result := make([]media.ListItem, 0, len(metadata))
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
