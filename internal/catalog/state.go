package catalog

import "context"

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
		out = append(out, s.states[key])
	}
	clear(s.changed)
	return out, nil
}

// publish marks key changed and wakes Updates. It never blocks. Callers hold s.mu.
func (s *Service) publish(key string, st State) {
	st.Fetching = s.active[key] != nil
	s.states[key] = st
	s.changed[key] = struct{}{}
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// accept records snapshot if it is newer than the published one. A validated
// snapshot clears the collection's error. Callers hold s.mu.
func (s *Service) accept(snapshot Snapshot) {
	key := snapshot.Resource.Key()
	st := s.states[key]
	if st.Known && (snapshot.Revision < st.Snapshot.Revision ||
		snapshot.Revision == st.Snapshot.Revision && snapshot.Stale == st.Snapshot.Stale && !snapshot.Validated) {
		return
	}
	st.Resource, st.Snapshot, st.Known = snapshot.Resource, snapshot, true
	if snapshot.Validated {
		st.Err = nil
	}
	s.publish(key, st)
}

// startAttempt records a new server request for r. Callers hold s.mu.
func (s *Service) startAttempt(r Resource) {
	st := s.states[r.Key()]
	st.Resource = r
	st.Attempt++
	st.Progress = Progress{}
	s.publish(r.Key(), st)
}

// refreshState republishes key after its flight or freshness changed.
// Callers hold s.mu.
func (s *Service) refreshState(key string, update func(*State)) {
	st, ok := s.states[key]
	if !ok {
		r, known := s.known[key]
		if !known {
			return // nothing has asked for this collection
		}
		st.Resource = r
	}
	if update != nil {
		update(&st)
	}
	s.publish(key, st)
}
