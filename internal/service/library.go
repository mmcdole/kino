package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/drake/goplex/internal/domain"
)

const (
	defaultPageSize = 50
	syncChunkSize   = 200 // Items per chunk during sync for responsive UI
)

// cachedResult stores cached data with timestamp
type cachedResult struct {
	Items     interface{}
	FetchedAt time.Time
}

// diskCacheEntry stores cached data with server timestamp for smart invalidation
type diskCacheEntry struct {
	Items           json.RawMessage `json:"items"`
	ServerUpdatedAt int64           `json:"serverUpdatedAt"`
}

// SyncProgress reports progress during library sync
type SyncProgress struct {
	LibraryID   string
	LibraryType string
	Loaded      int         // Items loaded so far
	Total       int         // Total items (from first response)
	Items       interface{} // Current chunk: []MediaItem or []Show
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
	cacheDir string // Disk cache directory
	diskMu   sync.Mutex
}

// NewLibraryService creates a new library service
func NewLibraryService(repo domain.LibraryRepository, logger *slog.Logger) *LibraryService {
	if logger == nil {
		logger = slog.Default()
	}

	// Set up disk cache directory
	cacheDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		cacheDir = filepath.Join(home, ".local", "share", "goplex", "cache")
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

// GetLibraries returns all available libraries
func (s *LibraryService) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	cacheKey := "libraries"

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

// GetLibraryDetails returns details for a specific library
func (s *LibraryService) GetLibraryDetails(ctx context.Context, libID string) (*domain.Library, error) {
	return s.repo.GetLibraryDetails(ctx, libID)
}

// SmartSync fetches library content with smart timestamp comparison
// Sends progress updates to the channel for live UI updates
// The channel is closed when sync is complete
func (s *LibraryService) SmartSync(
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

			if lib.Type == "movie" {
				var movies []domain.MediaItem
				if err := json.Unmarshal(entry.Items, &movies); err == nil {
					s.setCache(cacheKey, movies)
					progressCh <- SyncProgress{
						LibraryID:   lib.ID,
						LibraryType: lib.Type,
						Loaded:      len(movies),
						Total:       len(movies),
						Items:       movies,
						Done:        true,
						FromDisk:    true,
					}
					return
				}
			} else if lib.Type == "show" {
				var shows []domain.Show
				if err := json.Unmarshal(entry.Items, &shows); err == nil {
					s.setCache(cacheKey, shows)
					progressCh <- SyncProgress{
						LibraryID:   lib.ID,
						LibraryType: lib.Type,
						Loaded:      len(shows),
						Total:       len(shows),
						Items:       shows,
						Done:        true,
						FromDisk:    true,
					}
					return
				}
			}
		}
	}

	// Cache is stale or forced refresh - fetch from server with chunked streaming
	if lib.Type == "movie" {
		s.syncMovies(ctx, lib, lib.UpdatedAt, progressCh)
	} else {
		s.syncShows(ctx, lib, lib.UpdatedAt, progressCh)
	}
}

// syncMovies fetches movies in chunks and sends progress to channel
func (s *LibraryService) syncMovies(ctx context.Context, lib domain.Library, serverTS int64, progressCh chan<- SyncProgress) {
	cacheKey := "movies:" + lib.ID
	offset := 0
	var allMovies []domain.MediaItem

	for {
		movies, total, err := s.repo.GetMovies(ctx, lib.ID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get movies chunk", "error", err, "offset", offset)
			progressCh <- SyncProgress{LibraryID: lib.ID, LibraryType: lib.Type, Error: err}
			return
		}

		allMovies = append(allMovies, movies...)
		done := len(allMovies) >= total || len(movies) == 0

		progressCh <- SyncProgress{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Loaded:      len(allMovies),
			Total:       total,
			Items:       movies, // Just this chunk
			Done:        done,
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
	cacheKey := "shows:" + lib.ID
	offset := 0
	var allShows []domain.Show

	for {
		shows, total, err := s.repo.GetShows(ctx, lib.ID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get shows chunk", "error", err, "offset", offset)
			progressCh <- SyncProgress{LibraryID: lib.ID, LibraryType: lib.Type, Error: err}
			return
		}

		allShows = append(allShows, shows...)
		done := len(allShows) >= total || len(shows) == 0

		progressCh <- SyncProgress{
			LibraryID:   lib.ID,
			LibraryType: lib.Type,
			Loaded:      len(allShows),
			Total:       total,
			Items:       shows, // Just this chunk
			Done:        done,
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

// getCacheKeyForLib returns the cache key for a library's content
func (s *LibraryService) getCacheKeyForLib(lib domain.Library) string {
	if lib.Type == "movie" {
		return "movies:" + lib.ID
	}
	return "shows:" + lib.ID
}

// GetMovies returns all movies from a library
func (s *LibraryService) GetMovies(ctx context.Context, libID string) ([]domain.MediaItem, error) {
	cacheKey := "movies:" + libID

	// 1. Check memory cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("memory cache hit", "key", cacheKey)
		return cached.([]domain.MediaItem), nil
	}

	// 2. Check disk cache (with legacy format support)
	var movies []domain.MediaItem
	if s.loadFromDiskLegacy(cacheKey, &movies) {
		s.logger.Debug("disk cache hit", "key", cacheKey, "count", len(movies))
		s.setCache(cacheKey, movies) // Populate memory cache
		return movies, nil
	}

	// 3. Fetch from API using pagination loop
	offset := 0
	var allMovies []domain.MediaItem
	for {
		batch, total, err := s.repo.GetMovies(ctx, libID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get movies", "error", err, "libID", libID)
			return nil, err
		}

		allMovies = append(allMovies, batch...)
		if len(allMovies) >= total || len(batch) == 0 {
			break
		}
		offset += syncChunkSize
	}

	// 4. Store in both caches
	s.setCache(cacheKey, allMovies)
	s.saveToDisk(cacheKey, allMovies)
	s.logger.Info("loaded all movies", "count", len(allMovies), "libID", libID)

	return allMovies, nil
}

// GetShows returns all TV shows from a library
func (s *LibraryService) GetShows(ctx context.Context, libID string) ([]domain.Show, error) {
	cacheKey := "shows:" + libID

	// 1. Check memory cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("memory cache hit", "key", cacheKey)
		return cached.([]domain.Show), nil
	}

	// 2. Check disk cache (with legacy format support)
	var shows []domain.Show
	if s.loadFromDiskLegacy(cacheKey, &shows) {
		s.logger.Debug("disk cache hit", "key", cacheKey, "count", len(shows))
		s.setCache(cacheKey, shows) // Populate memory cache
		return shows, nil
	}

	// 3. Fetch from API using pagination loop
	offset := 0
	var allShows []domain.Show
	for {
		batch, total, err := s.repo.GetShows(ctx, libID, offset, syncChunkSize)
		if err != nil {
			s.logger.Error("failed to get shows", "error", err, "libID", libID)
			return nil, err
		}

		allShows = append(allShows, batch...)
		if len(allShows) >= total || len(batch) == 0 {
			break
		}
		offset += syncChunkSize
	}

	// 4. Store in both caches
	s.setCache(cacheKey, allShows)
	s.saveToDisk(cacheKey, allShows)
	s.logger.Info("loaded all shows", "count", len(allShows), "libID", libID)

	return allShows, nil
}

// GetCachedMovies returns movies only if cached in memory, nil otherwise (non-blocking)
func (s *LibraryService) GetCachedMovies(libID string) []domain.MediaItem {
	cacheKey := "movies:" + libID
	if cached, ok := s.getFromCache(cacheKey); ok {
		return cached.([]domain.MediaItem)
	}
	return nil
}

// GetCachedShows returns shows only if cached in memory, nil otherwise (non-blocking)
func (s *LibraryService) GetCachedShows(libID string) []domain.Show {
	cacheKey := "shows:" + libID
	if cached, ok := s.getFromCache(cacheKey); ok {
		return cached.([]domain.Show)
	}
	return nil
}

// GetSeasons returns all seasons for a TV show
func (s *LibraryService) GetSeasons(ctx context.Context, showID string) ([]domain.Season, error) {
	cacheKey := "seasons:" + showID

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]domain.Season), nil
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
func (s *LibraryService) GetEpisodes(ctx context.Context, seasonID string) ([]domain.MediaItem, error) {
	cacheKey := "episodes:" + seasonID

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]domain.MediaItem), nil
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

// GetOnDeck returns items from the "Continue Watching" section
func (s *LibraryService) GetOnDeck(ctx context.Context) ([]domain.MediaItem, error) {
	cacheKey := "ondeck"

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]domain.MediaItem), nil
	}

	// Fetch from repository
	items, err := s.repo.GetOnDeck(ctx)
	if err != nil {
		s.logger.Error("failed to get on deck", "error", err)
		return nil, err
	}

	// Store in cache
	s.setCache(cacheKey, items)
	s.logger.Debug("loaded on deck", "count", len(items))

	return items, nil
}

// GetRecentlyAdded returns recently added items from a library
func (s *LibraryService) GetRecentlyAdded(ctx context.Context, libID string, limit int) ([]domain.MediaItem, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}

	cacheKey := "recent:" + libID

	// Check cache
	if cached, ok := s.getFromCache(cacheKey); ok {
		s.logger.Debug("cache hit", "key", cacheKey)
		return cached.([]domain.MediaItem), nil
	}

	// Fetch from repository
	items, err := s.repo.GetRecentlyAdded(ctx, libID, limit)
	if err != nil {
		s.logger.Error("failed to get recently added", "error", err, "libID", libID)
		return nil, err
	}

	// Store in cache
	s.setCache(cacheKey, items)
	s.logger.Debug("loaded recently added", "count", len(items), "libID", libID)

	return items, nil
}

// RefreshLibrary clears cache for a specific library
func (s *LibraryService) RefreshLibrary(libID string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Clear all cache entries related to this library
	prefixes := []string{"movies:", "shows:", "recent:"}
	for _, prefix := range prefixes {
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

// InvalidateItem removes an item's parent container from cache
func (s *LibraryService) InvalidateItem(item domain.MediaItem) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Invalidate based on item type
	switch item.Type {
	case domain.MediaTypeMovie:
		delete(s.cache, "movies:"+item.LibraryID)
	case domain.MediaTypeEpisode:
		delete(s.cache, "episodes:"+item.ParentID)
	}

	// Also invalidate on deck
	delete(s.cache, "ondeck")

	s.logger.Debug("invalidated cache for item", "itemID", item.ID)
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
		Items:     items,
		FetchedAt: time.Now(),
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

// saveDiskCacheEntry saves data with server timestamp
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
	if err := os.WriteFile(path, entryBytes, 0644); err != nil {
		s.logger.Error("failed to write cache", "error", err, "path", path)
	}
}

// loadFromDiskLegacy loads cached data from disk (supports old format)
// Note: This is a legacy fallback for old cache format. No TTL check - the SmartSync
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

// saveToDisk saves data to disk cache (legacy format for backward compat)
func (s *LibraryService) saveToDisk(key string, data interface{}) {
	if s.cacheDir == "" {
		return
	}

	s.diskMu.Lock()
	defer s.diskMu.Unlock()

	path := s.diskCachePath(key)
	bytes, err := json.Marshal(data)
	if err != nil {
		s.logger.Error("failed to marshal cache", "error", err)
		return
	}
	if err := os.WriteFile(path, bytes, 0644); err != nil {
		s.logger.Error("failed to write cache", "error", err, "path", path)
	}
}

// clearDiskCache removes disk cache for a key
func (s *LibraryService) clearDiskCache(key string) {
	if s.cacheDir == "" {
		return
	}
	os.Remove(s.diskCachePath(key))
}
