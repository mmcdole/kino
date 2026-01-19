package plex

import (
	"fmt"
	"strconv"
	"time"

	"github.com/drake/goplex/internal/domain"
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

// MapLibrary converts a single Plex directory to a domain library
func MapLibrary(d Directory) *domain.Library {
	if d.Type != "movie" && d.Type != "show" {
		return nil
	}
	return &domain.Library{
		ID:        d.Key,
		Name:      d.Title,
		Type:      d.Type,
		UpdatedAt: d.ContentChangedAt,
	}
}

// MapMovies converts Plex metadata to domain media items (movies)
func MapMovies(metadata []Metadata, serverURL string) []domain.MediaItem {
	items := make([]domain.MediaItem, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "movie" {
			continue
		}
		items = append(items, mapMovie(m, serverURL))
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
		Duration:   time.Duration(m.Duration) * time.Millisecond,
		ViewOffset: time.Duration(m.ViewOffset) * time.Millisecond,
		ThumbURL:   buildThumbURL(serverURL, m.Thumb),
		Format:     extractFormat(m.Media),
		IsPlayed:   m.ViewCount > 0,
		Type:       domain.MediaTypeMovie,
	}

	if item.SortTitle == "" {
		item.SortTitle = item.Title
	}

	// Extract media URL if available
	if len(m.Media) > 0 && len(m.Media[0].Part) > 0 {
		item.MediaURL = buildMediaURL(serverURL, m.Media[0].Part[0].Key)
	}

	return item
}

// MapShows converts Plex metadata to domain shows
func MapShows(metadata []Metadata, serverURL string) []domain.Show {
	shows := make([]domain.Show, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "show" {
			continue
		}
		shows = append(shows, mapShow(m, serverURL))
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
		ThumbURL:       buildThumbURL(serverURL, m.Thumb),
		SeasonCount:    m.ChildCount,
		EpisodeCount:   m.LeafCount,
		UnwatchedCount: m.LeafCount - m.ViewedLeafCount,
	}

	if show.SortTitle == "" {
		show.SortTitle = show.Title
	}

	return show
}

// MapSeasons converts Plex metadata to domain seasons
func MapSeasons(metadata []Metadata, serverURL string) []domain.Season {
	seasons := make([]domain.Season, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "season" {
			continue
		}
		seasons = append(seasons, mapSeason(m, serverURL))
	}
	return seasons
}

// mapSeason converts a single season metadata to domain season
func mapSeason(m Metadata, serverURL string) domain.Season {
	return domain.Season{
		ID:             m.RatingKey,
		ShowID:         m.ParentRatingKey,
		ShowTitle:      m.ParentTitle,
		SeasonNum:      m.Index,
		Title:          m.Title,
		ThumbURL:       buildThumbURL(serverURL, m.Thumb),
		EpisodeCount:   m.LeafCount,
		UnwatchedCount: m.LeafCount - m.ViewedLeafCount,
	}
}

// MapEpisodes converts Plex metadata to domain media items (episodes)
func MapEpisodes(metadata []Metadata, serverURL string) []domain.MediaItem {
	items := make([]domain.MediaItem, 0, len(metadata))
	for _, m := range metadata {
		if m.Type != "episode" {
			continue
		}
		items = append(items, mapEpisode(m, serverURL))
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
		Duration:   time.Duration(m.Duration) * time.Millisecond,
		ViewOffset: time.Duration(m.ViewOffset) * time.Millisecond,
		ThumbURL:   buildThumbURL(serverURL, m.Thumb),
		Format:     extractFormat(m.Media),
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

	// Extract media URL if available
	if len(m.Media) > 0 && len(m.Media[0].Part) > 0 {
		item.MediaURL = buildMediaURL(serverURL, m.Media[0].Part[0].Key)
	}

	return item
}

// MapOnDeck converts Plex metadata to domain media items for On Deck
func MapOnDeck(metadata []Metadata, serverURL string) []domain.MediaItem {
	items := make([]domain.MediaItem, 0, len(metadata))
	for _, m := range metadata {
		switch m.Type {
		case "movie":
			items = append(items, mapMovie(m, serverURL))
		case "episode":
			items = append(items, mapEpisode(m, serverURL))
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

// extractFormat extracts the video codec from media info
func extractFormat(media []Media) string {
	if len(media) == 0 {
		return ""
	}

	codec := media[0].VideoCodec
	switch codec {
	case "hevc":
		return "HEVC"
	case "h264":
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
		return codec
	}
}

// buildThumbURL constructs a full thumbnail URL
func buildThumbURL(serverURL, path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("%s%s", serverURL, path)
}

// buildMediaURL constructs a full media URL
func buildMediaURL(serverURL, path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("%s%s", serverURL, path)
}
