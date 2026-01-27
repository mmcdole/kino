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

// LibraryStore implements domain.Store using BoltDB.
type LibraryStore struct {
	db *bolt.DB
	mu sync.RWMutex // Protects memory cache

	// In-memory cache for hot-path reads (promoted on access)
	cache map[string][]byte
}

func NewLibraryStore(baseCacheDir, serverURL string) (*LibraryStore, error) {
	if baseCacheDir == "" {
		// Memory-only mode (no persistence)
		return &LibraryStore{cache: make(map[string][]byte)}, nil
	}

	dir := baseCacheDir
	if serverURL != "" {
		dir = filepath.Join(baseCacheDir, hashServerURL(serverURL))
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
		for _, bucket := range [][]byte{bucketLibraries, bucketContent, bucketSeasons, bucketEpisodes, bucketPlaylists} {
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
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// === Generic helpers ===

func (s *LibraryStore) get(bucket []byte, key string, dest interface{}) bool {
	cacheKey := string(bucket) + ":" + key

	// Check memory cache first
	s.mu.RLock()
	if data, ok := s.cache[cacheKey]; ok {
		s.mu.RUnlock()
		return json.Unmarshal(data, dest) == nil
	}
	s.mu.RUnlock()

	if s.db == nil {
		return false
	}

	// Read from BoltDB
	var data []byte
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		if v := b.Get([]byte(key)); v != nil {
			data = make([]byte, len(v))
			copy(data, v)
		}
		return nil
	})

	if data == nil {
		return false
	}

	// Promote to memory cache
	s.mu.Lock()
	s.cache[cacheKey] = data
	s.mu.Unlock()

	return json.Unmarshal(data, dest) == nil
}

func (s *LibraryStore) set(bucket []byte, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	cacheKey := string(bucket) + ":" + key

	// Update memory cache
	s.mu.Lock()
	s.cache[cacheKey] = data
	s.mu.Unlock()

	if s.db == nil {
		return nil // Memory-only mode
	}

	// Write to BoltDB
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Put([]byte(key), data)
	})
}

func (s *LibraryStore) delete(bucket []byte, key string) {
	cacheKey := string(bucket) + ":" + key

	// Clear from memory cache
	s.mu.Lock()
	delete(s.cache, cacheKey)
	s.mu.Unlock()

	if s.db == nil {
		return
	}

	// Delete from BoltDB
	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b != nil {
			b.Delete([]byte(key))
		}
		return nil
	})
}

func (s *LibraryStore) deletePrefix(bucket []byte, prefix string) {
	// Clear from memory cache
	s.mu.Lock()
	cachePrefix := string(bucket) + ":" + prefix
	for k := range s.cache {
		if strings.HasPrefix(k, cachePrefix) {
			delete(s.cache, k)
		}
	}
	s.mu.Unlock()

	if s.db == nil {
		return
	}

	// Delete from BoltDB using prefix scan
	s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		prefixBytes := []byte(prefix)
		for k, _ := c.Seek(prefixBytes); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			if err := b.Delete(k); err != nil {
				return err
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
	// Save data
	if err := s.set(bucketContent, "lib:"+libID+":movies", movies); err != nil {
		return err
	}
	// Save timestamp separately for freshness checks
	return s.set(bucketContent, "lib:"+libID+":ts", serverTS)
}

// === Shows ===

func (s *LibraryStore) GetShows(libID string) ([]*domain.Show, bool) {
	var shows []*domain.Show
	ok := s.get(bucketContent, "lib:"+libID+":shows", &shows)
	return shows, ok
}

func (s *LibraryStore) SaveShows(libID string, shows []*domain.Show, serverTS int64) error {
	if err := s.set(bucketContent, "lib:"+libID+":shows", shows); err != nil {
		return err
	}
	return s.set(bucketContent, "lib:"+libID+":ts", serverTS)
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
	if err := s.set(bucketContent, "lib:"+libID+":mixed", wrapListItems(items)); err != nil {
		return err
	}
	return s.set(bucketContent, "lib:"+libID+":ts", serverTS)
}

// === Seasons (hierarchical key: lib:{libID}:show:{showID}) ===

func (s *LibraryStore) GetSeasons(libID, showID string) ([]*domain.Season, bool) {
	var seasons []*domain.Season
	key := fmt.Sprintf("lib:%s:show:%s", libID, showID)
	ok := s.get(bucketSeasons, key, &seasons)
	return seasons, ok
}

func (s *LibraryStore) SaveSeasons(libID, showID string, seasons []*domain.Season) error {
	key := fmt.Sprintf("lib:%s:show:%s", libID, showID)
	return s.set(bucketSeasons, key, seasons)
}

// === Episodes (hierarchical key: lib:{libID}:show:{showID}:season:{seasonID}) ===

func (s *LibraryStore) GetEpisodes(libID, showID, seasonID string) ([]*domain.MediaItem, bool) {
	var episodes []*domain.MediaItem
	key := fmt.Sprintf("lib:%s:show:%s:season:%s", libID, showID, seasonID)
	ok := s.get(bucketEpisodes, key, &episodes)
	return episodes, ok
}

func (s *LibraryStore) SaveEpisodes(libID, showID, seasonID string, episodes []*domain.MediaItem) error {
	key := fmt.Sprintf("lib:%s:show:%s:season:%s", libID, showID, seasonID)
	return s.set(bucketEpisodes, key, episodes)
}

// === Validation ===

func (s *LibraryStore) IsValid(libID string, serverTS int64) bool {
	var storedTS int64
	if !s.get(bucketContent, "lib:"+libID+":ts", &storedTS) {
		return false
	}
	return storedTS >= serverTS
}

// === Cascade Invalidation (hierarchical prefix deletion) ===

// InvalidateLibrary wipes library content + ALL seasons + ALL episodes in that library
func (s *LibraryStore) InvalidateLibrary(libID string) {
	prefix := "lib:" + libID + ":"
	// Delete movies/shows/mixed/ts for this library
	s.deletePrefix(bucketContent, prefix)
	// Delete all seasons for all shows in this library
	s.deletePrefix(bucketSeasons, prefix)
	// Delete all episodes for all seasons in this library
	s.deletePrefix(bucketEpisodes, prefix)
}

// InvalidateShow wipes a show's seasons + ALL episodes for that show
func (s *LibraryStore) InvalidateShow(libID, showID string) {
	prefix := fmt.Sprintf("lib:%s:show:%s", libID, showID)
	// Delete seasons for this show (exact key match)
	s.delete(bucketSeasons, prefix)
	// Delete all episodes for all seasons of this show (prefix match)
	s.deletePrefix(bucketEpisodes, prefix+":season:")
}

// InvalidateSeason wipes a season's episodes
func (s *LibraryStore) InvalidateSeason(libID, showID, seasonID string) {
	key := fmt.Sprintf("lib:%s:show:%s:season:%s", libID, showID, seasonID)
	s.delete(bucketEpisodes, key)
}

func (s *LibraryStore) InvalidateAll() {
	s.mu.Lock()
	s.cache = make(map[string][]byte)
	s.mu.Unlock()

	if s.db == nil {
		return
	}

	// Delete all data from all buckets
	s.db.Update(func(tx *bolt.Tx) error {
		for _, bucket := range [][]byte{bucketLibraries, bucketContent, bucketSeasons, bucketEpisodes, bucketPlaylists} {
			b := tx.Bucket(bucket)
			if b == nil {
				continue
			}
			c := b.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				if err := b.Delete(k); err != nil {
					return err
				}
			}
		}
		return nil
	})
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

func (s *LibraryStore) InvalidatePlaylists() {
	s.delete(bucketPlaylists, "list")
}

func (s *LibraryStore) InvalidatePlaylistItems(playlistID string) {
	s.delete(bucketPlaylists, "items:"+playlistID)
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
