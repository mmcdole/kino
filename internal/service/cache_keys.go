package service

// Cache key prefixes for library content
const (
	// PrefixLibraries is the cache key for the libraries list
	PrefixLibraries = "libraries"

	// PrefixMovies is the prefix for movie library caches (movies:{libID})
	PrefixMovies = "movies:"

	// PrefixShows is the prefix for TV show library caches (shows:{libID})
	PrefixShows = "shows:"

	// PrefixMixed is the prefix for mixed library caches (mixed:{libID})
	PrefixMixed = "mixed:"

	// PrefixSeasons is the prefix for show season caches (seasons:{showID})
	PrefixSeasons = "seasons:"

	// PrefixEpisodes is the prefix for season episode caches (episodes:{seasonID})
	PrefixEpisodes = "episodes:"

	// PrefixRecent is the prefix for recently added caches (recent:{libID})
	PrefixRecent = "recent:"
)

// LibraryCachePrefixes returns all cache key prefixes that should be invalidated
// when refreshing a library. This includes top-level library content but not
// nested content like seasons/episodes which are keyed by parent ID, not library ID.
func LibraryCachePrefixes() []string {
	return []string{PrefixMovies, PrefixShows, PrefixMixed, PrefixRecent}
}
