package store

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
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

func (s *Store) Load(key string) (domain.CachedList, bool) {
	var data storedSnapshot
	if !s.get(bucketSnapshots, key, &data) {
		return domain.CachedList{}, false
	}
	return domain.CachedList{Items: unwrapListItems(data.Items), FetchedAt: data.FetchedAt, Version: data.Version}, true
}

func (s *Store) Save(key string, data domain.CachedList) error {
	return s.set(bucketSnapshots, key, storedSnapshot{Items: wrapListItems(data.Items), FetchedAt: data.FetchedAt, Version: data.Version})
}

// Update hands every snapshot that mentions one of ids to fn and saves the
// snapshots fn returns, all in one transaction. It returns the saved keys.
// Matching is a cheap scan of the encoded data, so only candidates are decoded.
func (s *Store) Update(ids []string, fn func(map[string]domain.CachedList) map[string]domain.CachedList) ([]string, error) {
	needles := make([][]byte, len(ids))
	for i, id := range ids {
		needles[i], _ = json.Marshal(id)
	}
	mentions := func(data []byte) bool {
		return slices.ContainsFunc(needles, func(n []byte) bool { return bytes.Contains(data, n) })
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var saved []string
	if s.db == nil {
		prefix := string(bucketSnapshots) + ":"
		lists := make(map[string]domain.CachedList)
		for key, data := range s.cache {
			if name, ok := strings.CutPrefix(key, prefix); ok && mentions(data) {
				if l, ok := decodeSnapshot(data); ok {
					lists[name] = l
				}
			}
		}
		staged := make(map[string][]byte)
		for key, l := range fn(lists) {
			data, err := encodeSnapshot(l)
			if err != nil {
				return nil, err
			}
			staged[prefix+key] = data
			saved = append(saved, key)
		}
		maps.Copy(s.cache, staged)
		slices.Sort(saved)
		return saved, nil
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSnapshots)
		lists := make(map[string]domain.CachedList)
		err := b.ForEach(func(k, v []byte) error {
			if mentions(v) {
				if l, ok := decodeSnapshot(v); ok {
					lists[string(k)] = l
				}
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
	if json.Unmarshal(data, &snapshot) != nil {
		return domain.CachedList{}, false
	}
	return domain.CachedList{Items: unwrapListItems(snapshot.Items), FetchedAt: snapshot.FetchedAt, Version: snapshot.Version}, true
}

func encodeSnapshot(l domain.CachedList) ([]byte, error) {
	return json.Marshal(storedSnapshot{Items: wrapListItems(l.Items), FetchedAt: l.FetchedAt, Version: l.Version})
}
