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
