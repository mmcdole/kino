package catalog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// Backend and Cache describe the operations consumed by this application
// service. Concrete adapters remain independently testable.
type Backend interface {
	domain.LibraryClient
	domain.PlaylistClient
	domain.PlaybackClient
}

type Cache interface {
	Load(string) (domain.CachedList, bool)
	Save(string, domain.CachedList) error
	Remove(...string) error
	PatchWatchState(string, bool) error
}

type flight struct {
	resource  Resource
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	observers map[uint64]Observer
	result    Snapshot
	err       error
}

type Service struct {
	backend      Backend
	cache        Cache
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex // request ownership and commit fences; never acquired by the UI
	mutations    sync.Mutex
	active       map[string]*flight
	revisions    map[string]uint64
	known        map[string]Resource
	nextObserver uint64
	now          func() time.Time
}

func NewService(ctx context.Context, backend Backend, cache Cache) *Service {
	ctx, cancel := context.WithCancel(ctx)
	return &Service{backend: backend, cache: cache, ctx: ctx, cancel: cancel, active: make(map[string]*flight), revisions: make(map[string]uint64), known: make(map[string]Resource), now: time.Now}
}

// Close cancels outstanding I/O before the caller closes the cache.
func (s *Service) Close() { s.mu.Lock(); s.cancel(); s.mu.Unlock(); s.wg.Wait() }

func (s *Service) fresh(r Resource, entry domain.CachedList) bool {
	age := s.now().Sub(entry.FetchedAt)
	return !entry.FetchedAt.IsZero() && age >= 0 && age < MaxAge && (r.Version == 0 || entry.Version == r.Version)
}

// Load is the single browsing path. Cached data, foreground loads, startup
// sync, and explicit refresh use the same ownership and persistence rules.
func (s *Service) Load(ctx context.Context, r Resource, policy Policy, observer Observer) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	key := r.Key()
	published := false
	for {
		s.mu.Lock()
		if err := s.ctx.Err(); err != nil {
			s.mu.Unlock()
			return Snapshot{}, err
		}
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return Snapshot{}, err
		}
		if known, ok := s.known[key]; ok && known.Version > r.Version {
			r.Version = known.Version
		}
		s.known[key] = r
		entry, ok := s.cache.Load(key)
		cached := Snapshot{Resource: r, CachedList: entry, Revision: s.revisions[key], FromCache: true, Stale: !s.fresh(r, entry)}
		if old := s.active[key]; old != nil && old.resource.Version != r.Version {
			old.cancel()
			delete(s.active, key)
		}
		if policy == Refresh {
			if old := s.active[key]; old != nil {
				old.cancel()
				delete(s.active, key)
			}
		}
		current := s.active[key]
		if current == nil && policy == Browse && ok && !cached.Stale {
			s.mu.Unlock()
			return cached, nil
		}
		if current == nil {
			workCtx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
			current = &flight{resource: r, ctx: workCtx, cancel: cancel, done: make(chan struct{}), observers: make(map[uint64]Observer)}
			s.active[key] = current
			s.wg.Add(1)
			go s.run(r, policy, current)
		}
		s.nextObserver++
		observerID := s.nextObserver
		current.observers[observerID] = observer
		s.mu.Unlock()
		if ok && !published && observer.Cached != nil {
			observer.Cached(cached)
			published = true
		}
		select {
		case <-ctx.Done():
			s.release(key, current, observerID)
			return Snapshot{}, ctx.Err()
		case <-current.done:
			s.release(key, current, observerID)
			if ctx.Err() != nil {
				return Snapshot{}, ctx.Err()
			}
			if errors.Is(current.err, context.Canceled) && s.ctx.Err() == nil {
				// Another refresh or mutation superseded this work. Join its replacement
				// instead of failing a still-interested view or reviving an old request.
				policy = Browse
				continue
			}
			if current.err != nil && ok && !errors.Is(current.err, context.Canceled) && !errors.Is(current.err, domain.ErrItemNotFound) {
				cached.Stale = true
				return cached, current.err
			}
			return current.result.Clone(), current.err
		}
	}
}

func (s *Service) release(key string, f *flight, id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(f.observers, id)
	if len(f.observers) == 0 && s.active[key] == f {
		f.cancel()
		delete(s.active, key)
	}
}

func (s *Service) run(r Resource, policy Policy, f *flight) {
	defer s.wg.Done()
	defer f.cancel()
	progress := func(loaded, total int) {
		s.mu.Lock()
		callbacks := make([]func(Progress), 0, len(f.observers))
		for _, o := range f.observers {
			if o.Progress != nil {
				callbacks = append(callbacks, o.Progress)
			}
		}
		s.mu.Unlock()
		for _, callback := range callbacks {
			callback(Progress{loaded, total})
		}
	}
	// Counts are only an optimization while a complete snapshot is young.
	// A successful count check must not advance its fetched-at timestamp.
	var result Snapshot
	var err error
	s.mu.Lock()
	entry, ok := s.cache.Load(r.Key())
	canCheckCount := policy == Revalidate && ok && s.fresh(r, entry) && (r.Kind == Movies || r.Kind == Shows || r.Kind == Mixed)
	s.mu.Unlock()
	if canCheckCount {
		var count int
		count, err = s.backend.GetLibraryItemCount(f.ctx, r.LibraryID, libraryType(r.Kind))
		if err == nil && count == len(entry.Items) {
			result = Snapshot{Resource: r, CachedList: entry, FromCache: true}
		} else if err == nil {
			ok = false
		}
	} else {
		ok = false
	}
	if err == nil && !ok {
		result.Items, err = s.fetch(f.ctx, r, progress)
		result.Resource = r
		result.FetchedAt = s.now()
		result.Version = r.Version
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[r.Key()] != f {
		err = context.Canceled
	} else if f.ctx.Err() != nil {
		err = f.ctx.Err()
	} else if err == nil {
		s.revisions[r.Key()]++
		result.Revision = s.revisions[r.Key()]
		if !result.FromCache {
			if saveErr := s.cache.Save(r.Key(), result.CachedList); saveErr != nil {
				result.Warning = fmt.Errorf("cache write failed: %w", saveErr)
			}
		}
	}
	f.result, f.err = result, err
	if s.active[r.Key()] == f {
		delete(s.active, r.Key())
	}
	close(f.done)
}

func libraryType(kind Kind) string {
	switch kind {
	case Movies:
		return "movie"
	case Shows:
		return "show"
	default:
		return "mixed"
	}
}
