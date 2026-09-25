package catalog

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// Policy says how much Load trusts cached data.
type Policy uint8

const (
	// Browse serves a fresh snapshot; otherwise it revalidates after publishing
	// any stale snapshot. It joins an existing fetch for the same collection.
	Browse Policy = iota
	// Revalidate checks the server even when the snapshot has not expired.
	Revalidate
	// Refresh supersedes in-flight work and fetches a complete replacement.
	Refresh
)

// MaxAge bounds stale watch state and metadata even on servers with no useful
// revision. Reads, count checks, and local patches never extend this age.
const MaxAge = 5 * time.Minute

// ContentMaxAge bounds how long a library's item list is trusted on a count
// and server version check alone. Opening the library still refreshes it once
// MaxAge has passed, so watch state stays current where it is shown.
const ContentMaxAge = 24 * time.Hour

// flight is one server fetch, shared by every Load waiting on it.
type flight struct {
	resource    Resource
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	subscribers int
	result      Snapshot
	err         error
}

// Service owns every collection's cached snapshot, server fetch and
// published state, and applies writes to the server and the cache.
type Service struct {
	backend     Backend
	cache       Cache
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex // request ownership and revisions; never held across I/O
	commit      sync.Mutex // orders cache writes: fetch commits and mutation reconciliation
	mutations   chan struct{}
	collections map[string]*collection
	changed     map[string]struct{} // keys published since the last Updates
	signal      chan struct{}
	now         func() time.Time
}

// collection is everything the catalog tracks about one collection key.
// Entries are never removed, so a pointer stays valid; fields are guarded by
// s.mu.
type collection struct {
	key           string
	resource      Resource // as last requested, with the newest known version
	requested     bool     // Load has asked for it in this session
	flight        *flight  // the in-flight server fetch, if any
	revision      uint64   // advanced by every fetch commit and write fence
	cacheRevision uint64   // the revision the persisted payload matches
	invalid       bool     // the persisted payload cannot be trusted
	state         State    // last published
	published     bool
}

// track returns key's collection, creating it on first use. Callers hold s.mu.
func (s *Service) track(key string) *collection {
	c := s.collections[key]
	if c == nil {
		c = &collection{key: key}
		s.collections[key] = c
	}
	return c
}

// cancelFlight stops the in-flight fetch, if any, and reports whether there
// was one.
func (c *collection) cancelFlight() bool {
	if c.flight == nil {
		return false
	}
	c.flight.cancel()
	c.flight = nil
	return true
}

// fence cancels the in-flight fetch and advances the revision, so a response
// fetched before a write cannot undo it. It returns the new revision.
func (c *collection) fence() uint64 {
	c.cancelFlight()
	c.revision++
	return c.revision
}

// NewService starts a catalog whose work ends when ctx is done or Close is
// called.
func NewService(ctx context.Context, backend Backend, cache Cache) *Service {
	ctx, cancel := context.WithCancel(ctx)
	return &Service{
		backend: backend, cache: cache, ctx: ctx, cancel: cancel,
		mutations:   make(chan struct{}, 1),
		collections: make(map[string]*collection),
		changed:     make(map[string]struct{}),
		signal:      make(chan struct{}, 1),
		now:         time.Now,
	}
}

// Close cancels outstanding I/O before the caller closes the cache.
func (s *Service) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) fresh(r Resource, entry domain.CachedList) bool {
	return s.younger(r, entry, MaxAge)
}

// countCheckable reports whether revalidation may confirm a library's cached
// item list by its count instead of downloading it again.
func (s *Service) countCheckable(r Resource, entry domain.CachedList) bool {
	switch r.Kind {
	case Movies, Shows, Mixed:
		return s.younger(r, entry, ContentMaxAge)
	}
	return false
}

func (s *Service) younger(r Resource, entry domain.CachedList, limit time.Duration) bool {
	age := s.now().Sub(entry.FetchedAt)
	return !entry.FetchedAt.IsZero() && age >= 0 && age < limit && (r.Version == 0 || entry.Version == r.Version)
}

// Load is the single browsing path. Cached data, foreground loads, startup
// sync, and explicit refresh use the same ownership and persistence rules.
// It returns when the collection settles; every intermediate state, including
// cached data shown while the server is asked, is published through Updates.
func (s *Service) Load(ctx context.Context, r Resource, policy Policy) (Snapshot, error) {
	ctx, finish, err := s.operation(ctx, 0)
	if err != nil {
		return Snapshot{}, err
	}
	defer finish()

	key := r.Key()
	for {
		s.mu.Lock()
		if err := s.stopped(ctx); err != nil {
			s.mu.Unlock()
			return Snapshot{}, err
		}
		c := s.track(key)
		if c.requested && c.resource.Version > r.Version {
			r.Version = c.resource.Version
		}
		c.resource, c.requested = r, true
		// Register before decoding so a concurrent mutation fences this read.
		// Only detached cache data crosses the unlocked section.
		revision := c.revision
		s.mu.Unlock()
		entry, ok := s.cache.Load(key)
		s.mu.Lock()
		if revision != c.revision {
			s.mu.Unlock()
			continue
		}
		if err := s.stopped(ctx); err != nil {
			s.mu.Unlock()
			return Snapshot{}, err
		}
		cached := Snapshot{Resource: r, CachedList: entry, Revision: c.cacheRevision, fromCache: true, Stale: c.invalid || !s.fresh(r, entry)}
		if old := c.flight; old != nil && (policy == Refresh || old.resource.Version != r.Version) {
			c.cancelFlight()
		}
		if ok {
			s.accept(cached)
		}
		current := c.flight
		if current == nil && policy == Browse && ok && !cached.Stale {
			s.mu.Unlock()
			return cached, nil
		}
		if current == nil {
			workCtx, cancel := context.WithTimeout(s.ctx, r.Timeout())
			current = &flight{resource: r, ctx: workCtx, cancel: cancel, done: make(chan struct{})}
			c.flight = current
			s.startAttempt(c)
			s.wg.Add(1)
			go s.run(c, r, current, cached, policy == Revalidate && ok && !c.invalid && s.countCheckable(r, entry))
		}
		current.subscribers++
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			s.release(c, current)
			return Snapshot{}, ctx.Err()
		case <-current.done:
			s.release(c, current)
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

// stopped reports why work under ctx must end: the service closed or the
// caller gave up. The service context is checked first because ctx learns of
// Close asynchronously.
func (s *Service) stopped(ctx context.Context) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

// release drops one subscriber. The last one leaving cancels the fetch.
func (s *Service) release(c *collection, f *flight) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.subscribers--
	if f.subscribers == 0 && c.flight == f {
		c.cancelFlight()
		s.refreshState(c, nil)
	}
}

// run performs flight f for collection c and commits its result.
func (s *Service) run(c *collection, r Resource, f *flight, cached Snapshot, canCheckCount bool) {
	defer s.wg.Done()
	defer f.cancel()
	progress := func(loaded, total int) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c.flight == f {
			s.refreshState(c, func(st *State) { st.Progress = Progress{loaded, total} })
		}
	}
	// Counts are only an optimization while a complete snapshot is young.
	// A successful count check must not advance its fetched-at timestamp.
	var result Snapshot
	var err error
	entry := cached.CachedList
	ok := false
	if canCheckCount {
		var count int
		count, err = s.backend.GetLibraryItemCount(f.ctx, r.LibraryID, libraryType(r.Kind))
		if err == nil && count == len(entry.Items) {
			result = Snapshot{Resource: r, CachedList: entry, fromCache: true}
			ok = true
		}
	}
	if err == nil && !ok {
		result.Items, err = s.fetch(f.ctx, r, progress)
		result.Resource = r
		result.FetchedAt = s.now()
		result.Version = r.Version
	}
	// Writes are ordered by the commit lock, so the disk write happens without
	// holding s.mu and cache hits for other collections never wait on it.
	s.commit.Lock()
	defer s.commit.Unlock()
	s.mu.Lock()
	owned := c.flight == f && f.ctx.Err() == nil
	s.mu.Unlock()
	var saveErr error
	if owned && err == nil && !result.fromCache {
		saveErr = s.cache.Save(r.Key(), result.CachedList)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if c.flight != f {
		err = context.Canceled
	} else if f.ctx.Err() != nil {
		err = f.ctx.Err()
	} else if err == nil {
		result.Validated = true
		c.revision++
		result.Revision = c.revision
		if saveErr != nil {
			result.Warning = fmt.Errorf("cache write failed: %w", saveErr)
			c.invalid = true
		} else {
			c.cacheRevision = result.Revision
			c.invalid = false
		}
	}
	f.result, f.err = result, err
	if c.flight == f {
		c.flight = nil
		if err == nil {
			s.accept(result)
		}
		s.refreshState(c, func(st *State) {
			if err != nil && !errors.Is(err, context.Canceled) {
				st.Err = err
			}
		})
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

// operation makes every public operation part of the service lifetime, including
// cache reconciliation after a canceled remote write. Close waits for all of it.
func (s *Service) operation(parent context.Context, timeout time.Duration) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	} else {
		ctx, cancel = context.WithCancel(parent)
	}
	stop := context.AfterFunc(s.ctx, cancel)
	s.wg.Add(1)
	return ctx, func() { stop(); cancel(); s.wg.Done() }, nil
}
