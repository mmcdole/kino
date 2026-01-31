package plex

import (
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/domain"
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

	item.ContentRating = normalizeContentRating(m.ContentRating)
	if len(m.Media) > 0 {
		media := m.Media[0]
		item.Bitrate = media.Bitrate
		item.Width = media.Width
		item.Height = media.Height
		item.VideoCodec = normalizeCodec(media.VideoCodec)
		item.AudioCodec = normalizeAudioCodec(media.AudioCodec)
		item.AudioChannels = media.AudioChannels
		item.Container = normalizeContainer(media.Container)
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

	show.ContentRating = normalizeContentRating(m.ContentRating)

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

	item.ContentRating = normalizeContentRating(m.ContentRating)
	if len(m.Media) > 0 {
		media := m.Media[0]
		item.Bitrate = media.Bitrate
		item.Width = media.Width
		item.Height = media.Height
		item.VideoCodec = normalizeCodec(media.VideoCodec)
		item.AudioCodec = normalizeAudioCodec(media.AudioCodec)
		item.AudioChannels = media.AudioChannels
		item.Container = normalizeContainer(media.Container)
		if len(media.Part) > 0 {
			item.FileSize = media.Part[0].Size
		}
	}

	return item
}

// MapOnDeck converts Plex metadata to domain media items for On Deck
func MapOnDeck(metadata []Metadata, serverURL string) []*domain.MediaItem {
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

// MapMediaItem converts a single Plex metadata to domain media item
func MapMediaItem(m Metadata, serverURL string) domain.MediaItem {
	switch m.Type {
	case "movie":
		return mapMovie(m, serverURL)
	case "episode":
		return mapEpisode(m, serverURL)
	default:
		return domain.MediaItem{
			ID:    m.RatingKey,
			Title: m.Title,
		}
	}
}

// normalizeContentRating shortens verbose content rating strings
func normalizeContentRating(rating string) string {
	switch strings.ToLower(rating) {
	case "not rated", "unrated":
		return "NR"
	default:
		return rating
	}
}

// normalizeContainer cleans up the container format string
func normalizeContainer(container string) string {
	if container == "" {
		return ""
	}
	// Plex may return comma-separated list (e.g. "mov,mp4,m4a,3gp,3g2,mj2"); take first
	if i := strings.Index(container, ","); i >= 0 {
		container = container[:i]
	}
	return strings.ToLower(container)
}

// normalizeCodec converts video codec names to display format
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

// normalizeAudioCodec converts audio codec names to display format
func normalizeAudioCodec(codec string) string {
	switch strings.ToLower(codec) {
	case "aac":
		return "AAC"
	case "ac3":
		return "AC3"
	case "eac3":
		return "EAC3"
	case "dca", "dts":
		return "DTS"
	case "truehd":
		return "TrueHD"
	case "flac":
		return "FLAC"
	case "mp3":
		return "MP3"
	case "opus":
		return "Opus"
	case "vorbis":
		return "Vorbis"
	default:
		return strings.ToUpper(codec)
	}
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
