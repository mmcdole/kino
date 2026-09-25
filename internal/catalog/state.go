package catalog

import (
	"context"

	"github.com/mmcdole/kino/internal/domain"
)

// State is the catalog's current view of one collection. Published snapshots
// are never modified afterwards, so consumers may keep them without copying.
type State struct {
	Resource Resource
	Snapshot Snapshot
	Known    bool // Snapshot holds data, from the cache or the server
	Fetching bool // a server request for this collection is in flight
	Attempt  uint64
	Progress Progress
	Err      error // the last attempt's failure; only a validated result clears it
}

// Snapshot is one complete item list for a collection, with where it came
// from and how far it can be trusted.
type Snapshot struct {
	Resource Resource
	domain.CachedList
	Revision  uint64
	fromCache bool
	// Validated is true only when this result was checked against the server.
	// A count check can validate a payload that still comes from cache.
	Validated bool
	Stale     bool
	// Warning means usable data was fetched but could not be persisted.
	Warning error
}

// Clone copies the items so the caller can keep or change them.
func (s Snapshot) Clone() Snapshot { s.Items = domain.CloneItems(s.Items); return s }

// Progress reports pagination of a large collection.
type Progress struct{ Loaded, Total int }

// Updates blocks until at least one collection has changed since the previous
// call, then returns the latest state of each changed collection. States are
// complete, so a consumer that only keeps the latest never needs the ones in
// between.
func (s *Service) Updates(ctx context.Context) ([]State, error) {
	select {
	case <-s.signal:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]State, 0, len(s.changed))
	for key := range s.changed {
		out = append(out, s.collections[key].state)
	}
	clear(s.changed)
	return out, nil
}

// publish records st as c's state, marks it changed and wakes Updates. It
// never blocks. Callers hold s.mu.
func (s *Service) publish(c *collection, st State) {
	st.Fetching = c.flight != nil
	c.state, c.published = st, true
	s.changed[c.key] = struct{}{}
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// accept records snapshot if it is newer than the published one. A validated
// snapshot clears the collection's error. Callers hold s.mu.
func (s *Service) accept(snapshot Snapshot) {
	c := s.track(snapshot.Resource.Key())
	st := c.state
	if st.Known && (snapshot.Revision < st.Snapshot.Revision ||
		snapshot.Revision == st.Snapshot.Revision && snapshot.Stale == st.Snapshot.Stale && !snapshot.Validated) {
		return
	}
	st.Resource, st.Snapshot, st.Known = snapshot.Resource, snapshot, true
	if snapshot.Validated {
		st.Err = nil
	}
	s.publish(c, st)
}

// startAttempt records a new server request for c. Callers hold s.mu.
func (s *Service) startAttempt(c *collection) {
	st := c.state
	st.Resource = c.resource
	st.Attempt++
	st.Progress = Progress{}
	s.publish(c, st)
}

// refreshState republishes c after its flight or freshness changed.
// Callers hold s.mu.
func (s *Service) refreshState(c *collection, update func(*State)) {
	st := c.state
	if !c.published {
		if !c.requested {
			return // nothing has asked for this collection
		}
		st.Resource = c.resource
	}
	if update != nil {
		update(&st)
	}
	s.publish(c, st)
}
