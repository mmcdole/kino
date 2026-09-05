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

// listItemWrapper wraps ListItem for JSON serialization
type listItemWrapper struct {
	Type     string            `json:"type"`
	Movie    *domain.MediaItem `json:"movie,omitempty"`
	Show     *domain.Show      `json:"show,omitempty"`
	Season   *domain.Season    `json:"season,omitempty"`
	Library  *domain.Library   `json:"library,omitempty"`
	Playlist *domain.Playlist  `json:"playlist,omitempty"`
}

// Store uses BoltDB as the authoritative cache. Memory-only mode is
// used when persistence is unavailable. mu serializes complete mutations,
// including watch-state changes spanning several buckets.
type Store struct {
	db    *bolt.DB
	mu    sync.RWMutex
	cache map[string][]byte // used only in memory-only mode
}

// Open opens (or creates) the cache for one server+user pair.
// The user ID is part of the cache key: watch status, view offsets, and
// playlists are per-user, so two accounts on the same server must not share
// a cache. (Plex configs have no user ID; those stay keyed by URL alone.)
func Open(baseCacheDir, serverURL, userID string) (*Store, error) {
	if baseCacheDir == "" {
		// Memory-only mode (no persistence)
		return &Store{cache: make(map[string][]byte)}, nil
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

	// Snapshot schema replaces the old per-content buckets. This is a
	// disposable cache; obsolete entries are rebuilt on the next online load.
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketSnapshots); err != nil {
			return err
		}
		for _, name := range []string{"libraries", "content", "seasons", "episodes", "playlists"} {
			if tx.Bucket([]byte(name)) != nil {
				if err := tx.DeleteBucket([]byte(name)); err != nil {
					return err
				}
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

	return &Store{db: db, cache: make(map[string][]byte)}, nil
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

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) get(bucket []byte, key string, dest any) bool {
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

func (s *Store) set(bucket []byte, key string, value any) error {
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

type cacheDeletion struct {
	bucket []byte
	key    string
	prefix bool
}

// deleteEntries commits a cascade as a single mutation in either storage mode.
func (s *Store) deleteEntries(entries ...cacheDeletion) error {
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

func clampCount(n, max int) int {
	if n < 0 {
		return 0
	}
	if max > 0 && n > max {
		return max
	}
	return n
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
