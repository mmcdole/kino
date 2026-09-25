package components

// SortField represents a field to sort by
type SortField int

const (
	SortDefault SortField = iota
	SortTitle
	SortDateAdded
	SortLastUpdated // shows only
	SortReleased
	SortDuration
	SortRating
	SortEpisodeNum
)

// String returns the display name for the sort field
func (f SortField) String() string {
	switch f {
	case SortTitle:
		return "Title"
	case SortDateAdded:
		return "Date Added"
	case SortLastUpdated:
		return "Last Updated"
	case SortReleased:
		return "Release Date"
	case SortDuration:
		return "Duration"
	case SortRating:
		return "Rating"
	case SortEpisodeNum:
		return "Episode #"
	default:
		return "Unknown"
	}
}

// SortDirection represents sort direction
type SortDirection int

const (
	SortAsc SortDirection = iota
	SortDesc
)

// DefaultDirection returns the default sort direction for a field
func DefaultDirection(field SortField) SortDirection {
	switch field {
	case SortTitle:
		return SortAsc // A-Z
	case SortEpisodeNum:
		return SortAsc // natural order
	default:
		return SortDesc // newest/highest/longest first
	}
}

// MovieSortOptions returns the available sort options for movies
func MovieSortOptions() []SortField {
	return []SortField{SortTitle, SortDateAdded, SortReleased, SortDuration, SortRating}
}

// ShowSortOptions returns the available sort options for shows
func ShowSortOptions() []SortField {
	return []SortField{SortTitle, SortDateAdded, SortLastUpdated, SortReleased, SortRating}
}

// EpisodeSortOptions returns the available sort options for episodes
func EpisodeSortOptions() []SortField {
	return []SortField{SortEpisodeNum, SortTitle, SortDuration, SortDateAdded, SortRating}
}

// MixedSortOptions returns the available sort options for mixed content
func MixedSortOptions() []SortField {
	return []SortField{SortTitle, SortDateAdded, SortReleased, SortDuration, SortRating}
}
