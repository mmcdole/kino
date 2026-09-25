package store

import (
	"bytes"
	"encoding/json"
	"slices"
	"time"

	"github.com/mmcdole/kino/internal/domain"
	bolt "go.etcd.io/bbolt"
)

var bucketSnapshots = []byte("snapshots")

type storedSnapshot struct {
	Items     []listItemWrapper `json:"items"`
	FetchedAt time.Time         `json:"fetched_at"`
	Version   int64             `json:"version"`
}

// Load returns a detached copy of the snapshot stored under key.
func (s *Store) Load(key string) (domain.CachedList, bool) {
	var l domain.CachedList
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		l, ok = decodeSnapshot(tx.Bucket(bucketSnapshots).Get([]byte(key)))
		return nil
	})
	return l, ok
}

// Save replaces the snapshot stored under key.
func (s *Store) Save(key string, l domain.CachedList) error {
	data, err := encodeSnapshot(l)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSnapshots).Put([]byte(key), data)
	})
}

// Update hands every snapshot that mentions one of ids to fn and saves the
// snapshots fn returns, all in one transaction. It returns the saved keys.
// Matching is a cheap scan of the encoded data, so only candidates are decoded.
func (s *Store) Update(ids []string, fn func(map[string]domain.CachedList) map[string]domain.CachedList) ([]string, error) {
	needles := make([][]byte, len(ids))
	for i, id := range ids {
		needles[i], _ = json.Marshal(id)
	}
	var saved []string
	err := s.db.Update(func(tx *bolt.Tx) error {
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

func decodeSnapshot(data []byte) (domain.CachedList, bool) {
	var snapshot storedSnapshot
	if data == nil || json.Unmarshal(data, &snapshot) != nil {
		return domain.CachedList{}, false
	}
	return domain.CachedList{Items: unwrapListItems(snapshot.Items), FetchedAt: snapshot.FetchedAt, Version: snapshot.Version}, true
}

func encodeSnapshot(l domain.CachedList) ([]byte, error) {
	return json.Marshal(storedSnapshot{Items: wrapListItems(l.Items), FetchedAt: l.FetchedAt, Version: l.Version})
}
