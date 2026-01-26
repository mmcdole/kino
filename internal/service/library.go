package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mmcdole/kino/internal/domain"
)

const (
	syncChunkSize = 50 // Smaller batches to work around Jellyfin server issues
)

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
)

// LibraryCachePrefixes returns all cache key prefixes that should be invalidated
// when refreshing a library. This includes top-level library content but not
// nested content like seasons/episodes which are keyed by parent ID, not library ID.
func LibraryCachePrefixes() []string {
	return []string{PrefixMovies, PrefixShows, PrefixMixed}
}

// cachedResult stores cached data
type cachedResult struct {
	Items interface{}
}

// diskCacheEntry stores cached data with server timestamp for smart invalidation
type diskCacheEntry struct {
	Items           json.RawMessage `json:"items"`
	ServerUpdatedAt int64           `json:"serverUpdatedAt"`
}

// SyncChunk is a marker interface for typed sync chunks
type SyncChunk interface {
	syncChunk() // marker method
	ChunkSize() int
}

// MovieChunk is a chunk of movies during sync
type MovieChunk []*domain.MediaItem

func (MovieChunk) syncChunk() {}

// ChunkSize returns the number of items in the chunk
func (c MovieChunk) ChunkSize() int { return len(c) }

// ShowChunk is a chunk of shows during sync
type ShowChunk []*domain.Show

func (ShowChunk) syncChunk() {}

// ChunkSize returns the number of items in the chunk
func (c ShowChunk) ChunkSize() int { return len(c) }

// MixedChunk is a chunk of mixed content during sync
type MixedChunk []domain.ListItem

func (MixedChunk) syncChunk() {}

// ChunkSize returns the number of items in the chunk
func (c MixedChunk) ChunkSize() int { return len(c) }

// SyncProgress reports progress during library sync
type SyncProgress struct {
	LibraryID   string
	LibraryType string
	Loaded      int       // Items loaded so far
	Total       int       // Total items (from first response)
	Items       SyncChunk // Current chunk (MovieChunk, ShowChunk, or MixedChunk)
	Done        bool
	FromDisk    bool
	Error       error
}

// LibraryService handles library browsing with caching
// Cache invalidation is based on server timestamps, not TTL
type LibraryService struct {
	repo   domain.LibraryRepository
	logger *slog.Logger

	cache    map[string]cachedResult
	cacheMu  sync.RWMutex
	cacheDir string // Disk cache directory (namespaced by server URL)
	diskMu   sync.Mutex
}

// NewLibraryService creates a new library service
// serverURL is used to namespace the cache directory, preventing conflicts
// when switching between different servers
func NewLibraryService(repo domain.LibraryRepository, logger *slog.Logger, serverURL string) *LibraryService {
	if logger == nil {
		logger = slog.Default()
	}

	// Set up disk cache directory, namespaced by server URL hash
	cacheDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		baseCacheDir := filepath.Join(home, ".local", "share", "kino", "cache")

		// Hash the server URL to create a unique subdirectory
		if serverURL != "" {
			hash := hashServerURL(serverURL)
			cacheDir = filepath.Join(baseCacheDir, hash)
		} else {
			// Fallback for backwards compatibility (no server URL provided)
			cacheDir = baseCacheDir
		}

		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			logger.Warn("failed to create cache directory", "error", err)
			cacheDir = ""
		}
	}

	return &LibraryService{
		repo:     repo,
		logger:   logger,
		cache:    make(map[string]cachedResult),
		cacheDir: cacheDir,
	}
}

// hashServerURL creates a short hash of the server URL for cache namespacing
func hashServerURL(serverURL string) string {
	// Normalize URL (trim trailing slashes, lowercase)
	normalized := strings.TrimRight(strings.ToLower(serverURL), "/")
	hash := sha256.Sum256([]byte(normalized))
	// Use first 12 hex chars (6 bytes) - enough for uniqueness
	return hex.EncodeToString(hash[:6])
}

// GetLibraries returns all available libraries
func (s *LibraryService) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	cacheKey := PrefixLibraries

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]domain.Library), nil
	}

	// Fetch from repository
	libraries, err := s.repo.GetLibraries(ctx)
	if err != nil {
		s.logger.Error("failed to get libraries", "error", err)
		return nil, err
	}

	// Store in cache
	s.setCache(cacheKey, libraries)
	s.logger.Info("loaded libraries", "count", len(libraries))

	return libraries, nil
}

// SyncLibrary fetches library content with smart timestamp comparison
// Sends progress updates to the channel for live UI updates
// The channel is closed when sync is complete
func (s *LibraryService) SyncLibrary(
	ctx context.Context,
	lib domain.Library, // Already has UpdatedAt from GetLibraries()
	force bool,
	progressCh chan<- SyncProgress,
) {
	defer close(progressCh)

	cacheKey := s.getCacheKeyForLib(lib)

	// NOTE: lib.UpdatedAt is already populated from GetLibraries() call
	// No need to call GetLibraryDetails() - doing so for every library at startup
	// causes a "thundering herd" of API requests that overwhelm the server

	// Try to load from disk cache if not forced
	if !force {
		entry, ok := s.loadDiskCacheEntry(cacheKey)
		if ok && entry.ServerUpdatedAt >= lib.UpdatedAt {
			// Cache is still valid - use cached data
			s.logger.Debug("disk cache valid", "key", cacheKey, "cacheTS", entry.ServerUpdatedAt, "serverTS", lib.UpdatedAt)

			switch lib.Type {
			case "movie":
				var movies []*domain.MediaItem
				if err := json.Unmarshal(entry.Items, &movies); err == nil {
					s.setCache(cacheKey, movies)
					progressCh <- SyncProgress{
						LibraryID:   lib.ID,
						LibraryType: lib.Type,
						Loaded:      len(movies),
						Total:       len(movies),
						Items:       MovieChunk(movies),
						Done:        true,
						FromDisk:    true,
					}
					return
				}
			case "show":
				var shows []*domain.Show
				if err := json.Unmarshal(entry.Items, &shows); err == nil {
					s.setCache(cacheKey, shows)
					progressCh <- SyncProgress{
						LibraryID:   lib.ID,
						LibraryType: lib.Type,
						Loaded:      len(shows),
						Total:       len(shows),
						Items:       ShowChunk(shows),
						Done:        true,
						FromDisk:    true,
					}
					return
				}
			case "mixed":
				var items []domain.ListItem
				if err := json.Unmarshal(entry.Items, &items); err == nil {
					s.setCache(cacheKey, items)
					progressCh <- SyncProgress{
						LibraryID:   lib.ID,
						LibraryType: lib.Type,
						Loaded:      len(items),
						Total:       len(items),
						Items:       MixedChunk(items),
						Done:        true,
						FromDisk:    true,
					}
					return
				}
			}
		}
	}

	// Cache is stale or forced refresh - fetch from server with chunked streaming
	switch lib.Type {
	case "movie":
		s.syncMovies(ctx, lib, lib.UpdatedAt, progressCh)
	case "show":
		s.syncShows(ctx, lib, lib.UpdatedAt, progressCh)
	case "mixed":
		s.syncMixed(ctx, lib, lib.UpdatedAt, progressCh)
	default:
		// Unknown type - treat as mixed
		s.syncMixed(ctx, lib, lib.UpdatedAt, progressCh)
	}
}

// syncMovies fetches movies in chunks and sends progress to channel
// Skips failed batches and continues to get as much content as possible
func (s *LibraryService) syncMovies(ctx context.Context, lib domain.Library, serverTS int64, progressCh chan<- SyncProgress) {
	cacheKey := PrefixMovies + lib.ID
	offset := 0
	var allMovies []*domain.MediaItem
	var knownTotal int
	skippedBatches := 0

	for {
		// Check for cancellation before fetching
		select {
		case <-ctx.Done():
			s.logger.Info("sync cancelled", "libID", lib.ID)
			return
		default:
		}

		movies, total, err := s.repo.GetMovies(ctx, lib.ID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get movies chunk, skipping", "error", err, "offset", offset)
			skippedBatches++
			// Skip this batch and continue - don't fail the whole sync
			offset += syncChunkSize
			// If we know the total and we've passed it, we're done
			if knownTotal > 0 && offset >= knownTotal {
				break
			}
			// Safety limit: don't skip more than 10 batches in a row
			if skippedBatches > 10 {
				s.logger.Error("too many failed batches, aborting sync", "libID", lib.ID)
				progressCh <- SyncProgress{LibraryID: lib.ID, LibraryType: lib.Type, Error: err}
				return
			}
			continue
		}

		// Reset skip counter on success
		skippedBatches = 0

		// Remember total from first successful response
		if knownTotal == 0 {
			knownTotal = total
		}

		allMovies = append(allMovies, movies...)
		done := offset+len(movies) >= knownTotal || len(movies) == 0

		// Send progress, but check for cancellation
		select {
		case progressCh <- SyncProgress{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Loaded:      len(allMovies),
			Total:       knownTotal,
			Items:       MovieChunk(movies), // Just this chunk
			Done:        done,
		}:
		case <-ctx.Done():
			s.logger.Info("sync cancelled during progress send", "libID", lib.ID)
			return
		}

		if done {
			break
		}
		offset += syncChunkSize
	}

	// Store in both caches
	s.setCache(cacheKey, allMovies)
	s.saveDiskCacheEntry(cacheKey, allMovies, serverTS)
	s.logger.Info("synced movies", "count", len(allMovies), "libID", lib.ID)
}

// syncShows fetches shows in chunks and sends progress to channel
func (s *LibraryService) syncShows(ctx context.Context, lib domain.Library, serverTS int64, progressCh chan<- SyncProgress) {
	cacheKey := PrefixShows + lib.ID
	offset := 0
	var allShows []*domain.Show

	for {
		// Check for cancellation before fetching
		select {
		case <-ctx.Done():
			s.logger.Info("sync cancelled", "libID", lib.ID)
			return
		default:
		}

		shows, total, err := s.repo.GetShows(ctx, lib.ID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get shows chunk", "error", err, "offset", offset)
			progressCh <- SyncProgress{LibraryID: lib.ID, LibraryType: lib.Type, Error: err}
			return
		}

		allShows = append(allShows, shows...)
		done := len(allShows) >= total || len(shows) == 0

		// Send progress, but check for cancellation
		select {
		case progressCh <- SyncProgress{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Loaded:      len(allShows),
			Total:       total,
			Items:       ShowChunk(shows), // Just this chunk
			Done:        done,
		}:
		case <-ctx.Done():
			s.logger.Info("sync cancelled during progress send", "libID", lib.ID)
			return
		}

		if done {
			break
		}
		offset += syncChunkSize
	}

	// Store in both caches
	s.setCache(cacheKey, allShows)
	s.saveDiskCacheEntry(cacheKey, allShows, serverTS)
	s.logger.Info("synced shows", "count", len(allShows), "libID", lib.ID)
}

// syncMixed fetches mixed library content (movies AND shows) and sends progress to channel
func (s *LibraryService) syncMixed(ctx context.Context, lib domain.Library, serverTS int64, progressCh chan<- SyncProgress) {
	cacheKey := PrefixMixed + lib.ID
	offset := 0
	var allItems []domain.ListItem

	for {
		// Check for cancellation before fetching
		select {
		case <-ctx.Done():
			s.logger.Info("sync cancelled", "libID", lib.ID)
			return
		default:
		}

		items, total, err := s.repo.GetLibraryContent(ctx, lib.ID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get mixed content chunk", "error", err, "offset", offset)
			progressCh <- SyncProgress{LibraryID: lib.ID, LibraryType: lib.Type, Error: err}
			return
		}

		allItems = append(allItems, items...)
		done := len(allItems) >= total || len(items) == 0

		// Send progress, but check for cancellation
		select {
		case progressCh <- SyncProgress{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Loaded:      len(allItems),
			Total:       total,
			Items:       MixedChunk(items), // Just this chunk
			Done:        done,
		}:
		case <-ctx.Done():
			s.logger.Info("sync cancelled during progress send", "libID", lib.ID)
			return
		}

		if done {
			break
		}
		offset += syncChunkSize
	}

	// Store in both caches
	s.setCache(cacheKey, allItems)
	s.saveDiskCacheEntry(cacheKey, allItems, serverTS)
	s.logger.Info("synced mixed content", "count", len(allItems), "libID", lib.ID)
}

// getCacheKeyForLib returns the cache key for a library's content
func (s *LibraryService) getCacheKeyForLib(lib domain.Library) string {
	switch lib.Type {
	case "movie":
		return PrefixMovies + lib.ID
	case "show":
		return PrefixShows + lib.ID
	case "mixed":
		return PrefixMixed + lib.ID
	default:
		return PrefixMixed + lib.ID // Fallback for unknown types
	}
}

// GetMovies returns all movies from a library
func (s *LibraryService) GetMovies(ctx context.Context, libID string) ([]*domain.MediaItem, error) {
	cacheKey := PrefixMovies + libID

	// 1. Check memory cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("memory cache hit", "key", cacheKey)
		return cached.([]*domain.MediaItem), nil
	}

	// 2. Check disk cache (with legacy format support)
	var movies []*domain.MediaItem
	if s.loadFromDiskLegacy(cacheKey, &movies) {
		s.logger.Debug("disk cache hit", "key", cacheKey, "count", len(movies))
		s.setCache(cacheKey, movies) // Populate memory cache
		return movies, nil
	}

	// 3. Fetch from API (adapter handles pagination)
	allMovies, err := s.repo.GetAllMovies(ctx, libID)
	if err != nil {
		s.logger.Error("failed to get movies", "error", err, "libID", libID)
		return nil, err
	}

	// 4. Store in both caches
	s.setCache(cacheKey, allMovies)
	s.saveToDisk(cacheKey, allMovies)
	s.logger.Info("loaded all movies", "count", len(allMovies), "libID", libID)

	return allMovies, nil
}

// GetShows returns all TV shows from a library
func (s *LibraryService) GetShows(ctx context.Context, libID string) ([]*domain.Show, error) {
	cacheKey := PrefixShows + libID

	// 1. Check memory cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("memory cache hit", "key", cacheKey)
		return cached.([]*domain.Show), nil
	}

	// 2. Check disk cache (with legacy format support)
	var shows []*domain.Show
	if s.loadFromDiskLegacy(cacheKey, &shows) {
		s.logger.Debug("disk cache hit", "key", cacheKey, "count", len(shows))
		s.setCache(cacheKey, shows) // Populate memory cache
		return shows, nil
	}

	// 3. Fetch from API (adapter handles pagination)
	allShows, err := s.repo.GetAllShows(ctx, libID)
	if err != nil {
		s.logger.Error("failed to get shows", "error", err, "libID", libID)
		return nil, err
	}

	// 4. Store in both caches
	s.setCache(cacheKey, allShows)
	s.saveToDisk(cacheKey, allShows)
	s.logger.Info("loaded all shows", "count", len(allShows), "libID", libID)

	return allShows, nil
}

// GetCachedMovies returns movies only if cached in memory, nil otherwise (non-blocking)
func (s *LibraryService) GetCachedMovies(libID string) []*domain.MediaItem {
	cacheKey := PrefixMovies + libID
	if cached, ok := s.getFromCache(cacheKey); ok {
		return cached.([]*domain.MediaItem)
	}
	return nil
}

// GetCachedShows returns shows only if cached in memory, nil otherwise (non-blocking)
func (s *LibraryService) GetCachedShows(libID string) []*domain.Show {
	cacheKey := PrefixShows + libID
	if cached, ok := s.getFromCache(cacheKey); ok {
		return cached.([]*domain.Show)
	}
	return nil
}

// GetLibraryContent returns all content (movies AND shows) from a mixed library
func (s *LibraryService) GetLibraryContent(ctx context.Context, libID string) ([]domain.ListItem, error) {
	cacheKey := PrefixMixed + libID

	// 1. Check memory cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("memory cache hit", "key", cacheKey)
		return cached.([]domain.ListItem), nil
	}

	// 2. Check disk cache (with legacy format support)
	var items []domain.ListItem
	if s.loadFromDiskLegacy(cacheKey, &items) {
		s.logger.Debug("disk cache hit", "key", cacheKey, "count", len(items))
		s.setCache(cacheKey, items) // Populate memory cache
		return items, nil
	}

	// 3. Fetch from API (adapter handles pagination)
	allItems, err := s.repo.GetAllLibraryContent(ctx, libID)
	if err != nil {
		s.logger.Error("failed to get library content", "error", err, "libID", libID)
		return nil, err
	}

	// 4. Store in both caches
	s.setCache(cacheKey, allItems)
	s.saveToDisk(cacheKey, allItems)
	s.logger.Info("loaded all library content", "count", len(allItems), "libID", libID)

	return allItems, nil
}

// GetCachedLibraryContent returns mixed library content only if cached in memory, nil otherwise (non-blocking)
func (s *LibraryService) GetCachedLibraryContent(libID string) []domain.ListItem {
	cacheKey := PrefixMixed + libID
	if cached, ok := s.getFromCache(cacheKey); ok {
		return cached.([]domain.ListItem)
	}
	return nil
}

// GetSeasons returns all seasons for a TV show
func (s *LibraryService) GetSeasons(ctx context.Context, showID string) ([]*domain.Season, error) {
	cacheKey := PrefixSeasons + showID

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]*domain.Season), nil
	}

	// Fetch from repository
	seasons, err := s.repo.GetSeasons(ctx, showID)
	if err != nil {
		s.logger.Error("failed to get seasons", "error", err, "showID", showID)
		return nil, err
	}

	// Store in cache
	s.setCache(cacheKey, seasons)
	s.logger.Debug("loaded seasons", "count", len(seasons), "showID", showID)

	return seasons, nil
}

// GetEpisodes returns all episodes for a season
func (s *LibraryService) GetEpisodes(ctx context.Context, seasonID string) ([]*domain.MediaItem, error) {
	cacheKey := PrefixEpisodes + seasonID

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]*domain.MediaItem), nil
	}

	// Fetch from repository
	episodes, err := s.repo.GetEpisodes(ctx, seasonID)
	if err != nil {
		s.logger.Error("failed to get episodes", "error", err, "seasonID", seasonID)
		return nil, err
	}

	// Store in cache
	s.setCache(cacheKey, episodes)
	s.logger.Debug("loaded episodes", "count", len(episodes), "seasonID", seasonID)

	return episodes, nil
}

// RefreshLibrary clears cache for a specific library
func (s *LibraryService) RefreshLibrary(libID string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Clear all cache entries related to this library
	for _, prefix := range LibraryCachePrefixes() {
		key := prefix + libID
		delete(s.cache, key)
		s.clearDiskCache(key) // Also clear disk cache
	}

	s.logger.Info("cleared cache for library", "libID", libID)
}

// RefreshAll clears all cached data
func (s *LibraryService) RefreshAll() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Clear disk cache for all keys
	for key := range s.cache {
		s.clearDiskCache(key)
	}

	s.cache = make(map[string]cachedResult)
	s.logger.Info("cleared all cache")
}

// getFromCache retrieves an item from memory cache
// Memory cache is invalidated via RefreshLibrary/RefreshAll, not by TTL
func (s *LibraryService) getFromCache(key string) (interface{}, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	cached, ok := s.cache[key]
	if !ok {
		return nil, false
	}

	return cached.Items, true
}

// setCache stores an item in cache
func (s *LibraryService) setCache(key string, items interface{}) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	s.cache[key] = cachedResult{
		Items: items,
	}
}


// diskCachePath returns the path for a cache file
func (s *LibraryService) diskCachePath(key string) string {
	// Sanitize key for filename (replace : with _)
	safe := strings.ReplaceAll(key, ":", "_")
	return filepath.Join(s.cacheDir, safe+".json")
}

// loadDiskCacheEntry loads the new cache format with server timestamp
func (s *LibraryService) loadDiskCacheEntry(key string) (*diskCacheEntry, bool) {
	if s.cacheDir == "" {
		return nil, false
	}

	s.diskMu.Lock()
	defer s.diskMu.Unlock()

	path := s.diskCachePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry diskCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		// Might be old format, return false to trigger re-fetch
		return nil, false
	}

	// Validate that items field is not empty
	if len(entry.Items) == 0 {
		return nil, false
	}

	return &entry, true
}

// saveDiskCacheEntry saves data with server timestamp using atomic write
func (s *LibraryService) saveDiskCacheEntry(key string, data interface{}, serverTS int64) {
	if s.cacheDir == "" {
		return
	}

	s.diskMu.Lock()
	defer s.diskMu.Unlock()

	itemsBytes, err := json.Marshal(data)
	if err != nil {
		s.logger.Error("failed to marshal items", "error", err)
		return
	}

	entry := diskCacheEntry{
		Items:           itemsBytes,
		ServerUpdatedAt: serverTS,
	}

	entryBytes, err := json.Marshal(entry)
	if err != nil {
		s.logger.Error("failed to marshal cache entry", "error", err)
		return
	}

	path := s.diskCachePath(key)
	tmpPath := path + ".tmp"

	// Write to temp file first
	if err := os.WriteFile(tmpPath, entryBytes, 0644); err != nil {
		s.logger.Error("failed to write temp cache", "error", err, "path", tmpPath)
		return
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		s.logger.Error("failed to rename cache file", "error", err, "path", path)
		os.Remove(tmpPath) // Clean up temp file
		return
	}
}

// loadFromDiskLegacy loads cached data from disk (supports old format)
// Note: This is a legacy fallback for old cache format. No TTL check - the SyncLibrary
// mechanism handles cache invalidation via server timestamp comparison.
func (s *LibraryService) loadFromDiskLegacy(key string, target interface{}) bool {
	if s.cacheDir == "" {
		return false
	}

	s.diskMu.Lock()
	defer s.diskMu.Unlock()

	path := s.diskCachePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	// Try new format first
	var entry diskCacheEntry
	if err := json.Unmarshal(data, &entry); err == nil && len(entry.Items) > 0 {
		return json.Unmarshal(entry.Items, target) == nil
	}

	// Fall back to legacy format (plain JSON array)
	return json.Unmarshal(data, target) == nil
}

// saveToDisk saves data to disk cache using atomic write (legacy format for backward compat)
func (s *LibraryService) saveToDisk(key string, data interface{}) {
	if s.cacheDir == "" {
		return
	}

	s.diskMu.Lock()
	defer s.diskMu.Unlock()

	path := s.diskCachePath(key)
	tmpPath := path + ".tmp"

	bytes, err := json.Marshal(data)
	if err != nil {
		s.logger.Error("failed to marshal cache", "error", err)
		return
	}

	if err := os.WriteFile(tmpPath, bytes, 0644); err != nil {
		s.logger.Error("failed to write temp cache", "error", err, "path", tmpPath)
		return
	}

	if err := os.Rename(tmpPath, path); err != nil {
		s.logger.Error("failed to rename cache file", "error", err, "path", path)
		os.Remove(tmpPath)
		return
	}
}

// clearDiskCache removes disk cache for a key
func (s *LibraryService) clearDiskCache(key string) {
	if s.cacheDir == "" {
		return
	}
	os.Remove(s.diskCachePath(key))
}
