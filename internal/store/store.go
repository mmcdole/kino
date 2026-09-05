package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	bolt "go.etcd.io/bbolt"
)

// Bucket names
var (
	bucketLibraries = []byte("libraries")
	bucketContent   = []byte("content")
	bucketSeasons   = []byte("seasons")
	bucketEpisodes  = []byte("episodes")
	bucketPlaylists = []byte("playlists")
)

// listItemWrapper wraps ListItem for JSON serialization
type listItemWrapper struct {
	Type     string            `json:"type"`
	Movie    *domain.MediaItem `json:"movie,omitempty"`
	Show     *domain.Show      `json:"show,omitempty"`
	Season   *domain.Season    `json:"season,omitempty"`
	Library  *domain.Library   `json:"library,omitempty"`
	Playlist *domain.Playlist  `json:"playlist,omitempty"`
}

// LibraryStore uses BoltDB as the authoritative cache. Memory-only mode is
// used when persistence is unavailable. mu serializes complete mutations,
// including watch-state changes spanning several buckets.
type LibraryStore struct {
	db    *bolt.DB
	mu    sync.RWMutex
	cache map[string][]byte // used only in memory-only mode
}

// NewLibraryStore opens (or creates) the cache for one server+user pair.
// The user ID is part of the cache key: watch status, view offsets, and
// playlists are per-user, so two accounts on the same server must not share
// a cache. (Plex configs have no user ID; those stay keyed by URL alone.)
func NewLibraryStore(baseCacheDir, serverURL, userID string) (*LibraryStore, error) {
	if baseCacheDir == "" {
		// Memory-only mode (no persistence)
		return &LibraryStore{cache: make(map[string][]byte)}, nil
	}

	dir := baseCacheDir
	if serverURL != "" {
		dir = filepath.Join(baseCacheDir, hashServerURL(serverURL+"|"+userID))
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dir, "kino.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open bolt db: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{bucketLibraries, bucketContent, bucketSeasons, bucketEpisodes, bucketPlaylists, bucketSnapshots} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	// Clean up legacy JSON cache files from pre-BoltDB era
	cleanupLegacyJSONCache(dir)

	return &LibraryStore{db: db, cache: make(map[string][]byte)}, nil
}

func hashServerURL(serverURL string) string {
	normalized := strings.TrimRight(strings.ToLower(serverURL), "/")
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:6])
}

// cleanupLegacyJSONCache removes vestigial JSON cache files from pre-BoltDB era.
func cleanupLegacyJSONCache(cacheDir string) {
	matches, err := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if err != nil || len(matches) == 0 {
		return
	}
	for _, path := range matches {
		os.Remove(path) // Ignore errors
	}
}

func (s *LibraryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *LibraryStore) get(bucket []byte, key string, dest any) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return json.Unmarshal(s.cache[string(bucket)+":"+key], dest) == nil
	}
	// Decode inside the read transaction; no promoted copy can outlive deletion.
	return s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return fmt.Errorf("cache bucket missing")
		}
		return json.Unmarshal(b.Get([]byte(key)), dest)
	}) == nil
}

func (s *LibraryStore) set(bucket []byte, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(bucket).Put([]byte(key), data) })
	}
	s.cache[string(bucket)+":"+key] = data
	return nil
}

func (s *LibraryStore) setContentPair(dataKey string, value any, tsKey string, serverTS int64) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tsData, err := json.Marshal(serverTS)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(bucketContent)
			if err := b.Put([]byte(dataKey), data); err != nil {
				return err
			}
			return b.Put([]byte(tsKey), tsData)
		})
	}
	s.cache[string(bucketContent)+":"+dataKey] = data
	s.cache[string(bucketContent)+":"+tsKey] = tsData
	return nil
}

type cacheDeletion struct {
	bucket []byte
	key    string
	prefix bool
}

// deleteEntries commits a cascade as a single mutation in either storage mode.
func (s *LibraryStore) deleteEntries(entries ...cacheDeletion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		for _, entry := range entries {
			prefix := string(entry.bucket) + ":" + entry.key
			for key := range s.cache {
				if key == prefix || (entry.prefix && strings.HasPrefix(key, prefix)) {
					delete(s.cache, key)
				}
			}
		}
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, entry := range entries {
			b := tx.Bucket(entry.bucket)
			if !entry.prefix {
				if err := b.Delete([]byte(entry.key)); err != nil {
					return err
				}
				continue
			}
			cursor := b.Cursor()
			for k, _ := cursor.Seek([]byte(entry.key)); k != nil && strings.HasPrefix(string(k), entry.key); k, _ = cursor.Next() {
				if err := cursor.Delete(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// === Libraries ===

func (s *LibraryStore) GetLibraries() ([]domain.Library, bool) {
	var libs []domain.Library
	ok := s.get(bucketLibraries, "list", &libs)
	return libs, ok
}

func (s *LibraryStore) SaveLibraries(libs []domain.Library) error {
	return s.set(bucketLibraries, "list", libs)
}

// === Movies ===

func (s *LibraryStore) GetMovies(libID string) ([]*domain.MediaItem, bool) {
	var movies []*domain.MediaItem
	ok := s.get(bucketContent, "lib:"+libID+":movies", &movies)
	return movies, ok
}

func (s *LibraryStore) SaveMovies(libID string, movies []*domain.MediaItem, serverTS int64) error {
	return s.setContentPair("lib:"+libID+":movies", movies, "lib:"+libID+":ts", serverTS)
}

// === Shows ===

func (s *LibraryStore) GetShows(libID string) ([]*domain.Show, bool) {
	var shows []*domain.Show
	ok := s.get(bucketContent, "lib:"+libID+":shows", &shows)
	return shows, ok
}

func (s *LibraryStore) SaveShows(libID string, shows []*domain.Show, serverTS int64) error {
	return s.setContentPair("lib:"+libID+":shows", shows, "lib:"+libID+":ts", serverTS)
}

// === Mixed Content ===

func (s *LibraryStore) GetMixedContent(libID string) ([]domain.ListItem, bool) {
	var wrappers []listItemWrapper
	if !s.get(bucketContent, "lib:"+libID+":mixed", &wrappers) {
		return nil, false
	}
	return unwrapListItems(wrappers), true
}

func (s *LibraryStore) SaveMixedContent(libID string, items []domain.ListItem, serverTS int64) error {
	return s.setContentPair("lib:"+libID+":mixed", wrapListItems(items), "lib:"+libID+":ts", serverTS)
}

// === Seasons (hierarchical key: lib:{libID}:show:{showID}) ===

// tvCacheTTL bounds staleness of the TV hierarchy caches. Unlike libraries
// (which have a server timestamp plus an item-count check), seasons and
// episodes expose no server-side freshness signal at all, so without a TTL
// they would be served stale forever — new episodes never appearing until a
// manual refresh.
const tvCacheTTL = 6 * time.Hour

// timestamped wraps hierarchical cache payloads with their fetch time.
// Pre-TTL cache entries fail to decode into this wrapper and simply read as
// cache misses.
type timestamped struct {
	FetchedAt int64           `json:"fetched_at"`
	Data      json.RawMessage `json:"data"`
}

func (s *LibraryStore) getWithTTL(bucket []byte, key string, dest interface{}) bool {
	var wrapper timestamped
	if !s.get(bucket, key, &wrapper) {
		return false
	}
	if time.Now().Unix()-wrapper.FetchedAt > int64(tvCacheTTL.Seconds()) {
		return false
	}
	return json.Unmarshal(wrapper.Data, dest) == nil
}

func (s *LibraryStore) setWithTTL(bucket []byte, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.set(bucket, key, timestamped{FetchedAt: time.Now().Unix(), Data: data})
}

func (s *LibraryStore) GetSeasons(libID, showID string) ([]*domain.Season, bool) {
	var seasons []*domain.Season
	key := fmt.Sprintf("lib:%s:show:%s", libID, showID)
	ok := s.getWithTTL(bucketSeasons, key, &seasons)
	return seasons, ok
}

func (s *LibraryStore) SaveSeasons(libID, showID string, seasons []*domain.Season) error {
	key := fmt.Sprintf("lib:%s:show:%s", libID, showID)
	return s.setWithTTL(bucketSeasons, key, seasons)
}

// === Episodes (hierarchical key: lib:{libID}:show:{showID}:season:{seasonID}) ===

func (s *LibraryStore) GetEpisodes(libID, showID, seasonID string) ([]*domain.MediaItem, bool) {
	var episodes []*domain.MediaItem
	key := fmt.Sprintf("lib:%s:show:%s:season:%s", libID, showID, seasonID)
	ok := s.getWithTTL(bucketEpisodes, key, &episodes)
	return episodes, ok
}

func (s *LibraryStore) SaveEpisodes(libID, showID, seasonID string, episodes []*domain.MediaItem) error {
	key := fmt.Sprintf("lib:%s:show:%s:season:%s", libID, showID, seasonID)
	return s.setWithTTL(bucketEpisodes, key, episodes)
}

// === Validation ===

func (s *LibraryStore) IsValid(libID string, serverTS int64) bool {
	var storedTS int64
	if !s.get(bucketContent, "lib:"+libID+":ts", &storedTS) {
		return false
	}
	return storedTS >= serverTS
}

// === In-place watch state updates ===

// SetWatchState patches a media item's watch state in place everywhere it is
// cached (library lists, episode lists, mixed content, playlist items) and
// adjusts the containing season/show unwatched counters. Cached data stays
// warm — nothing is invalidated; the next real sync reconciles with the
// server.
func (s *LibraryStore) SetWatchState(itemID string, played bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	apply := func(tx *bolt.Tx, memory map[string][]byte) error {
		var updateErr error
		updateEach := func(bucket []byte, filter func(string) bool, transform func(string, []byte) []byte) {
			if updateErr == nil {
				updateErr = updateEach(tx, memory, bucket, filter, transform)
			}
		}

		var flipped bool
		var showID, seasonID string

		patch := func(m *domain.MediaItem) bool {
			if m == nil || m.ID != itemID {
				return false
			}
			if m.IsPlayed != played {
				flipped = true
				if m.ShowID != "" {
					showID = m.ShowID
					seasonID = m.ParentID
				}
			}
			m.IsPlayed = played
			m.ViewOffset = 0
			return true
		}

		// []*MediaItem payloads: movie lists, episode lists, playlist items
		patchItemList := func(key string, data []byte) []byte {
			var items []*domain.MediaItem
			if json.Unmarshal(data, &items) != nil {
				return nil
			}
			changed := false
			for _, m := range items {
				if patch(m) {
					changed = true
				}
			}
			if !changed {
				return nil
			}
			out, err := json.Marshal(items)
			if err != nil {
				return nil
			}
			return out
		}

		updateEach(bucketEpisodes, nil, wrapped(patchItemList))
		updateEach(bucketContent, keySuffix(":movies"), patchItemList)
		updateEach(bucketPlaylists, keyPrefix("items:"), patchItemList)
		updateEach(bucketContent, keySuffix(":mixed"), func(key string, data []byte) []byte {
			var wrappers []listItemWrapper
			if json.Unmarshal(data, &wrappers) != nil {
				return nil
			}
			changed := false
			for i := range wrappers {
				if patch(wrappers[i].Movie) {
					changed = true
				}
			}
			if !changed {
				return nil
			}
			out, err := json.Marshal(wrappers)
			if err != nil {
				return nil
			}
			return out
		})

		// Adjust unwatched counters on the containing season and show
		if !flipped || showID == "" {
			return updateErr
		}
		delta := 1
		if played {
			delta = -1
		}

		updateEach(bucketSeasons, nil, wrapped(func(key string, data []byte) []byte {
			var seasons []*domain.Season
			if json.Unmarshal(data, &seasons) != nil {
				return nil
			}
			changed := false
			for _, season := range seasons {
				if season != nil && season.ID == seasonID {
					season.UnwatchedCount = clampCount(season.UnwatchedCount+delta, season.EpisodeCount)
					changed = true
				}
			}
			if !changed {
				return nil
			}
			out, err := json.Marshal(seasons)
			if err != nil {
				return nil
			}
			return out
		}))

		adjustShow := func(show *domain.Show) bool {
			if show == nil || show.ID != showID {
				return false
			}
			show.UnwatchedCount = clampCount(show.UnwatchedCount+delta, show.EpisodeCount)
			return true
		}
		updateEach(bucketContent, keySuffix(":shows"), func(key string, data []byte) []byte {
			var shows []*domain.Show
			if json.Unmarshal(data, &shows) != nil {
				return nil
			}
			changed := false
			for _, show := range shows {
				if adjustShow(show) {
					changed = true
				}
			}
			if !changed {
				return nil
			}
			out, err := json.Marshal(shows)
			if err != nil {
				return nil
			}
			return out
		})
		updateEach(bucketContent, keySuffix(":mixed"), func(key string, data []byte) []byte {
			var wrappers []listItemWrapper
			if json.Unmarshal(data, &wrappers) != nil {
				return nil
			}
			changed := false
			for i := range wrappers {
				if adjustShow(wrappers[i].Show) {
					changed = true
				}
			}
			if !changed {
				return nil
			}
			out, err := json.Marshal(wrappers)
			if err != nil {
				return nil
			}
			return out
		})
		return updateErr
	}
	if s.db != nil {
		return s.db.Update(func(tx *bolt.Tx) error { return apply(tx, nil) })
	}
	// Stage memory changes just like a transaction so failures cannot partly apply.
	staged := make(map[string][]byte, len(s.cache))
	for key, value := range s.cache {
		staged[key] = value
	}
	if err := apply(nil, staged); err != nil {
		return err
	}
	s.cache = staged
	return nil
}

func clampCount(n, max int) int {
	if n < 0 {
		return 0
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

func keySuffix(suffix string) func(string) bool {
	return func(k string) bool { return strings.HasSuffix(k, suffix) }
}

// wrapped adapts a payload transform to values stored inside the timestamped
// TTL wrapper (seasons/episodes), preserving the original fetch time.
func wrapped(transform func(key string, data []byte) []byte) func(key string, data []byte) []byte {
	return func(key string, data []byte) []byte {
		var w timestamped
		if json.Unmarshal(data, &w) != nil || w.Data == nil {
			return nil
		}
		newData := transform(key, w.Data)
		if newData == nil {
			return nil
		}
		w.Data = newData
		out, err := json.Marshal(w)
		if err != nil {
			return nil
		}
		return out
	}
}

func keyPrefix(prefix string) func(string) bool {
	return func(k string) bool { return strings.HasPrefix(k, prefix) }
}

// updateEach reads and transforms values within the caller's write transaction.
func updateEach(tx *bolt.Tx, memory map[string][]byte, bucket []byte, filter func(string) bool, transform func(string, []byte) []byte) error {
	if tx != nil {
		b := tx.Bucket(bucket)
		var keys []string
		if err := b.ForEach(func(k, v []byte) error {
			if filter == nil || filter(string(k)) {
				keys = append(keys, string(k))
			}
			return nil
		}); err != nil {
			return err
		}
		for _, key := range keys {
			if data := transform(key, b.Get([]byte(key))); data != nil {
				if err := b.Put([]byte(key), data); err != nil {
					return err
				}
			}
		}
		return nil
	}
	prefix := string(bucket) + ":"
	for key, value := range memory {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := strings.TrimPrefix(key, prefix)
		if filter != nil && !filter(name) {
			continue
		}
		if data := transform(name, value); data != nil {
			memory[key] = data
		}
	}
	return nil
}

func (s *LibraryStore) InvalidateLibrary(libID string) error {
	prefix := "lib:" + libID + ":"
	return s.deleteEntries(cacheDeletion{bucketContent, prefix, true}, cacheDeletion{bucketSeasons, prefix, true}, cacheDeletion{bucketEpisodes, prefix, true})
}
func (s *LibraryStore) InvalidateShow(libID, showID string) error {
	prefix := fmt.Sprintf("lib:%s:show:%s", libID, showID)
	return s.deleteEntries(cacheDeletion{bucketSeasons, prefix, false}, cacheDeletion{bucketEpisodes, prefix + ":season:", true})
}
func (s *LibraryStore) InvalidateSeason(libID, showID, seasonID string) error {
	key := fmt.Sprintf("lib:%s:show:%s:season:%s", libID, showID, seasonID)
	return s.deleteEntries(cacheDeletion{bucketEpisodes, key, false})
}
func (s *LibraryStore) InvalidateAll() error {
	return s.deleteEntries(cacheDeletion{bucketLibraries, "", true}, cacheDeletion{bucketContent, "", true}, cacheDeletion{bucketSeasons, "", true}, cacheDeletion{bucketEpisodes, "", true}, cacheDeletion{bucketPlaylists, "", true})
}

// === Playlists ===

func (s *LibraryStore) GetPlaylists() ([]*domain.Playlist, bool) {
	var playlists []*domain.Playlist
	ok := s.get(bucketPlaylists, "list", &playlists)
	return playlists, ok
}

func (s *LibraryStore) SavePlaylists(playlists []*domain.Playlist) error {
	return s.set(bucketPlaylists, "list", playlists)
}

func (s *LibraryStore) GetPlaylistItems(playlistID string) ([]*domain.MediaItem, bool) {
	var items []*domain.MediaItem
	ok := s.get(bucketPlaylists, "items:"+playlistID, &items)
	return items, ok
}

func (s *LibraryStore) SavePlaylistItems(playlistID string, items []*domain.MediaItem) error {
	return s.set(bucketPlaylists, "items:"+playlistID, items)
}

func (s *LibraryStore) InvalidatePlaylists() error {
	return s.deleteEntries(cacheDeletion{bucketPlaylists, "list", false})
}

func (s *LibraryStore) InvalidatePlaylistItems(playlistID string) error {
	return s.deleteEntries(cacheDeletion{bucketPlaylists, "items:" + playlistID, false})
}

// wrapListItems converts domain.ListItem slice to serializable wrappers
func wrapListItems(items []domain.ListItem) []listItemWrapper {
	wrappers := make([]listItemWrapper, len(items))
	for i, item := range items {
		switch v := item.(type) {
		case *domain.MediaItem:
			wrappers[i] = listItemWrapper{Type: "movie", Movie: v}
		case *domain.Show:
			wrappers[i] = listItemWrapper{Type: "show", Show: v}
		case *domain.Season:
			wrappers[i] = listItemWrapper{Type: "season", Season: v}
		case *domain.Library:
			wrappers[i] = listItemWrapper{Type: "library", Library: v}
		case *domain.Playlist:
			wrappers[i] = listItemWrapper{Type: "playlist", Playlist: v}
		}
	}
	return wrappers
}

// unwrapListItems converts wrappers back to domain.ListItem slice
func unwrapListItems(wrappers []listItemWrapper) []domain.ListItem {
	items := make([]domain.ListItem, 0, len(wrappers))
	for _, w := range wrappers {
		switch w.Type {
		case "movie":
			if w.Movie != nil {
				items = append(items, w.Movie)
			}
		case "show":
			if w.Show != nil {
				items = append(items, w.Show)
			}
		case "season":
			if w.Season != nil {
				items = append(items, w.Season)
			}
		case "library":
			if w.Library != nil {
				items = append(items, w.Library)
			}
		case "playlist":
			if w.Playlist != nil {
				items = append(items, w.Playlist)
			}
		}
	}
	return items
}
