package components

import (
	"github.com/mmcdole/kino/internal/domain"
)

// Conversion functions to convert domain slices to []domain.ListItem

// WrapLibraries converts a slice of domain.Library to []domain.ListItem
func WrapLibraries(libs []domain.Library) []domain.ListItem {
	items := make([]domain.ListItem, len(libs))
	for i := range libs {
		items[i] = &libs[i]
	}
	return items
}

// WrapMovies converts a slice of *domain.MediaItem (movies) to []domain.ListItem
func WrapMovies(movies []*domain.MediaItem) []domain.ListItem {
	items := make([]domain.ListItem, len(movies))
	for i, m := range movies {
		items[i] = m
	}
	return items
}

// WrapShows converts a slice of *domain.Show to []domain.ListItem
func WrapShows(shows []*domain.Show) []domain.ListItem {
	items := make([]domain.ListItem, len(shows))
	for i, s := range shows {
		items[i] = s
	}
	return items
}

// WrapSeasons converts a slice of *domain.Season to []domain.ListItem
func WrapSeasons(seasons []*domain.Season) []domain.ListItem {
	items := make([]domain.ListItem, len(seasons))
	for i, s := range seasons {
		items[i] = s
	}
	return items
}

// WrapEpisodes converts a slice of *domain.MediaItem (episodes) to []domain.ListItem
func WrapEpisodes(episodes []*domain.MediaItem) []domain.ListItem {
	items := make([]domain.ListItem, len(episodes))
	for i, e := range episodes {
		items[i] = e
	}
	return items
}

// WrapPlaylists converts a slice of *domain.Playlist to []domain.ListItem
func WrapPlaylists(playlists []*domain.Playlist) []domain.ListItem {
	items := make([]domain.ListItem, len(playlists))
	for i, p := range playlists {
		items[i] = p
	}
	return items
}

// WrapPlaylistItems converts a slice of *domain.MediaItem (playlist items) to []domain.ListItem
func WrapPlaylistItems(items []*domain.MediaItem) []domain.ListItem {
	result := make([]domain.ListItem, len(items))
	for i, item := range items {
		result[i] = item
	}
	return result
}

