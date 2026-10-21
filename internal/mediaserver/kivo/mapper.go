package kivo

import (
	"time"

	"github.com/mmcdole/kino/internal/media"
	"github.com/mmcdole/kino/internal/mediaserver/normalize"
)

// libraryType maps a Kivo library kind to kino's. Audiobooks libraries (books
// by author and series) and videos libraries (folders of loose videos) have
// no columns in kino yet, so they are left out.
func libraryType(kind string) (string, bool) {
	switch kind {
	case "movies":
		return "movie", true
	case "shows":
		return "show", true
	default:
		return "", false
	}
}

func mapLibraries(libs []library) []media.Library {
	out := make([]media.Library, 0, len(libs))
	for _, l := range libs {
		t, ok := libraryType(l.Kind)
		if !ok {
			continue
		}
		out = append(out, media.Library{ID: l.ID, Name: l.Name, Type: t})
	}
	return out
}

func millis(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

func mapItem(w work, libID string) *media.Item {
	item := &media.Item{
		ID:            w.ID,
		Title:         w.Title,
		LibraryID:     libID,
		Year:          w.Year,
		Duration:      millis(w.DurationMs),
		Rating:        w.CommunityRating,
		ContentRating: normalize.ContentRating(w.ContentRating),
		Type:          media.TypeMovie,
	}
	if !w.AddedAt.IsZero() {
		item.AddedAt = w.AddedAt.Unix()
	}
	if w.UserState != nil {
		item.IsPlayed = w.UserState.Played
		item.ViewOffset = millis(w.UserState.PositionMs)
	}
	if e := w.Episode; e != nil {
		item.Type = media.TypeEpisode
		item.ShowTitle = e.SeriesTitle
		item.ShowID = e.SeriesID
		item.SeasonNum = e.Season
		item.EpisodeNum = e.Number
		item.ParentID = e.SeasonID
	}
	return item
}

// mapShow maps a series card. Cards carry the unplayed count but not the
// episode count, which stays zero: unknown.
func mapShow(w work, libID string) *media.Show {
	show := &media.Show{
		ID:            w.ID,
		Title:         w.Title,
		LibraryID:     libID,
		Year:          w.Year,
		Rating:        w.CommunityRating,
		ContentRating: normalize.ContentRating(w.ContentRating),
	}
	if !w.AddedAt.IsZero() {
		show.AddedAt = w.AddedAt.Unix()
	}
	if s := w.Series; s != nil {
		show.SeasonCount = s.SeasonCount
		show.UnwatchedCount = s.UnplayedCount
	}
	return show
}

func mapSeason(w work) *media.Season {
	season := &media.Season{ID: w.ID, Title: w.Title}
	if s := w.Season; s != nil {
		season.ShowID = s.SeriesID
		season.ShowTitle = s.SeriesTitle
		season.SeasonNum = s.Number
		season.EpisodeCount = s.EpisodeCount
		season.UnwatchedCount = s.UnplayedCount
	}
	return season
}
