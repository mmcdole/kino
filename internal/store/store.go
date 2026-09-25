package store

import (
	"fmt"
	"os"
	"path/filepath"
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

// Store is the persistent snapshot cache, backed by BoltDB. Bolt serializes
// writers and gives readers consistent views, so Store needs no locking.
type Store struct {
	db *bolt.DB
}

// Open opens (or creates) the cache database in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dir, "kino.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open bolt db: %w", err)
	}

	// The snapshots bucket is authoritative. Discard other collection buckets
	// so reads cannot mix incompatible schemas.
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

	// JSON cache files are not read by this store.
	cleanupLegacyJSONCache(dir)

	return &Store{db: db}, nil
}

// cleanupLegacyJSONCache removes JSON files that are not used by snapshot storage.
func cleanupLegacyJSONCache(cacheDir string) {
	matches, err := filepath.Glob(filepath.Join(cacheDir, "*.json"))
	if err != nil || len(matches) == 0 {
		return
	}
	for _, path := range matches {
		os.Remove(path) // Ignore errors
	}
}

// Close waits for open transactions and closes the database.
func (s *Store) Close() error {
	return s.db.Close()
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
