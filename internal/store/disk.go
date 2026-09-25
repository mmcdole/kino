package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	bolt "go.etcd.io/bbolt"
)

// bucketSnapshots holds every collection payload, keyed by collection.
var bucketSnapshots = []byte("snapshots")

// Disk is the persistent snapshot cache, backed by BoltDB. Bolt serializes
// writers and gives readers consistent views, so Disk needs no locking.
type Disk struct {
	db *bolt.DB
}

// OpenDisk opens (or creates) the cache database in dir.
func OpenDisk(dir string) (*Disk, error) {
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

	return &Disk{db: db}, nil
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
func (d *Disk) Close() error {
	return d.db.Close()
}

// Load returns a detached copy of the snapshot stored under key.
func (d *Disk) Load(key string) (domain.CachedList, bool) {
	var l domain.CachedList
	ok := false
	d.db.View(func(tx *bolt.Tx) error {
		l, ok = decodeSnapshot(tx.Bucket(bucketSnapshots).Get([]byte(key)))
		return nil
	})
	return l, ok
}

// Save replaces the snapshot stored under key.
func (d *Disk) Save(key string, l domain.CachedList) error {
	data, err := encodeSnapshot(l)
	if err != nil {
		return err
	}
	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSnapshots).Put([]byte(key), data)
	})
}

// Update hands every snapshot that mentions one of ids to fn and saves the
// snapshots fn returns, all in one transaction. It returns the saved keys.
// Matching is a cheap scan of the encoded data, so only candidates are decoded.
func (d *Disk) Update(ids []string, fn func(map[string]domain.CachedList) map[string]domain.CachedList) ([]string, error) {
	needles := make([][]byte, len(ids))
	for i, id := range ids {
		needles[i], _ = json.Marshal(id)
	}
	var saved []string
	err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSnapshots)
		lists := make(map[string]domain.CachedList)
		err := b.ForEach(func(k, v []byte) error {
			mentioned := slices.ContainsFunc(needles, func(n []byte) bool { return bytes.Contains(v, n) })
			if l, ok := decodeSnapshot(v); mentioned && ok {
				lists[string(k)] = l
			}
			return nil
		})
		if err != nil {
			return err
		}
		for key, l := range fn(lists) {
			data, err := encodeSnapshot(l)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(key), data); err != nil {
				return err
			}
			saved = append(saved, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(saved)
	return saved, nil
}
