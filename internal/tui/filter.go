package tui

import (
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/service"
)

// NavigationContext contains information needed to navigate to an item
// This is purely a TUI concern - the service layer provides FilterItem with LibraryID,
// and the TUI decides how to navigate based on that.
type NavigationContext struct {
	LibraryID   string
	LibraryName string
	MovieID     string
	ShowID      string
	ShowTitle   string
	SeasonID    string
	SeasonNum   int
	EpisodeID   string
}

// buildNavContext constructs navigation context from a filter result
func (m *Model) buildNavContext(item service.FilterItem) NavigationContext {
	lib := m.findLibrary(item.LibraryID)
	libName := ""
	if lib != nil {
		libName = lib.Name
	}

	ctx := NavigationContext{
		LibraryID:   item.LibraryID,
		LibraryName: libName,
	}

	switch item.Type {
	case domain.MediaTypeMovie:
		ctx.MovieID = item.Item.GetID()
	case domain.MediaTypeShow:
		ctx.ShowID = item.Item.GetID()
		if show, ok := item.Item.(*domain.Show); ok {
			ctx.ShowTitle = show.Title
		}
	}

	return ctx
}
