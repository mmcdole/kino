package components

// ColumnType identifies the type of content in a column
type ColumnType int

const (
	ColumnTypeLibraries ColumnType = iota
	ColumnTypeMovies
	ColumnTypeShows
	ColumnTypeMixed // Mixed content (movies + shows)
	ColumnTypeSeasons
	ColumnTypeEpisodes
	ColumnTypePlaylists
	ColumnTypePlaylistItems
	ColumnTypeEmpty
)
