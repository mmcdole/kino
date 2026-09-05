package store

import (
	"encoding/json"
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

func (s *LibraryStore) Load(key string) (domain.CachedList, bool) {
	var data storedSnapshot
	if !s.get(bucketSnapshots, key, &data) {
		return domain.CachedList{}, false
	}
	return domain.CachedList{Items: unwrapListItems(data.Items), FetchedAt: data.FetchedAt, Version: data.Version}, true
}

func (s *LibraryStore) Save(key string, data domain.CachedList) error {
	return s.set(bucketSnapshots, key, storedSnapshot{Items: wrapListItems(data.Items), FetchedAt: data.FetchedAt, Version: data.Version})
}

func (s *LibraryStore) Remove(keys ...string) error {
	entries := make([]cacheDeletion, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, cacheDeletion{bucketSnapshots, key, false})
	}
	return s.deleteEntries(entries...)
}

// PatchWatchState updates every cached projection in one transaction. Parent
// counters are adjusted once, even when an episode occurs in several lists.
func (s *LibraryStore) PatchWatchState(itemID string, played bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	apply := func(tx *bolt.Tx, memory map[string][]byte) error {
		var showID, seasonID string
		flipped := false
		patch := func(key string, data []byte) []byte {
			var snapshot storedSnapshot
			if json.Unmarshal(data, &snapshot) != nil {
				return nil
			}
			changed := false
			for i := range snapshot.Items {
				item := snapshot.Items[i].Movie
				if item == nil || item.ID != itemID {
					continue
				}
				if item.IsPlayed != played {
					flipped = true
					showID = item.ShowID
					seasonID = item.ParentID
				}
				item.IsPlayed = played
				item.ViewOffset = 0
				changed = true
			}
			if !changed {
				return nil
			}
			out, _ := json.Marshal(snapshot)
			return out
		}
		if err := updateEach(tx, memory, bucketSnapshots, nil, patch); err != nil {
			return err
		}
		if !flipped || showID == "" {
			return nil
		}
		delta := 1
		if played {
			delta = -1
		}
		return updateEach(tx, memory, bucketSnapshots, nil, func(key string, data []byte) []byte {
			var snapshot storedSnapshot
			if json.Unmarshal(data, &snapshot) != nil {
				return nil
			}
			changed := false
			for _, item := range snapshot.Items {
				if item.Show != nil && item.Show.ID == showID {
					item.Show.UnwatchedCount = clampCount(item.Show.UnwatchedCount+delta, item.Show.EpisodeCount)
					changed = true
				}
				if item.Season != nil && item.Season.ID == seasonID {
					item.Season.UnwatchedCount = clampCount(item.Season.UnwatchedCount+delta, item.Season.EpisodeCount)
					changed = true
				}
			}
			if !changed {
				return nil
			}
			out, _ := json.Marshal(snapshot)
			return out
		})
	}
	if s.db != nil {
		return s.db.Update(func(tx *bolt.Tx) error { return apply(tx, nil) })
	}
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
