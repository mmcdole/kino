package store

import (
	"maps"
	"slices"
	"sync"

	"github.com/mmcdole/kino/internal/domain"
)

// Memory is a snapshot cache that lasts for the life of the process. It is
// used when the disk cache cannot be opened. Values are copied in and out, so
// callers never share entities with the cache.
type Memory struct {
	mu        sync.RWMutex
	snapshots map[string]domain.CachedList
}

func NewMemory() *Memory {
	return &Memory{snapshots: make(map[string]domain.CachedList)}
}

func (m *Memory) Load(key string) (domain.CachedList, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.snapshots[key]
	return detach(l), ok
}

func (m *Memory) Save(key string, l domain.CachedList) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshots[key] = detach(l)
	return nil
}

// Update hands every snapshot to fn and saves the ones it returns. The memory
// cache is small enough that it does not filter by ids.
func (m *Memory) Update(ids []string, fn func(map[string]domain.CachedList) map[string]domain.CachedList) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lists := make(map[string]domain.CachedList, len(m.snapshots))
	for key, l := range m.snapshots {
		lists[key] = detach(l)
	}
	changed := fn(lists)
	for key, l := range changed {
		m.snapshots[key] = detach(l)
	}
	return slices.Sorted(maps.Keys(changed)), nil
}

func (m *Memory) Close() error { return nil }

func detach(l domain.CachedList) domain.CachedList {
	l.Items = domain.CloneItems(l.Items)
	return l
}
