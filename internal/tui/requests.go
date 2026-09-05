package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/components"
)

// These are the application operations the TUI consumes. There is no cache or
// backend access here: all data reaches the model through a scoped result.
type Catalog interface {
	Load(context.Context, catalog.Resource, catalog.Policy, catalog.Observer) (catalog.Snapshot, error)
	Mutate(context.Context, catalog.Mutation) (catalog.Change, error)
	PlaylistMembership(context.Context, string) (catalog.Membership, error)
}
type Playback interface {
	Play(context.Context, domain.MediaItem) error
	Resume(context.Context, domain.MediaItem) error
}

type request struct {
	ID       uint64
	Owner    string
	Resource catalog.Resource
	Policy   catalog.Policy
	ctx      context.Context
	cancel   context.CancelFunc
}

// requests is owned by the event loop. IDs apply to errors, progress, and
// successes alike. Canceling a view detaches it from shared service work.
type requests struct {
	ctx    context.Context
	cancel context.CancelFunc
	next   uint64
	active map[string]request
}

func newRequests(ctx context.Context) *requests {
	ctx, cancel := context.WithCancel(ctx)
	return &requests{ctx: ctx, cancel: cancel, active: make(map[string]request)}
}
func (rs *requests) begin(owner string, r catalog.Resource, policy catalog.Policy) request {
	rs.stop(owner)
	rs.next++
	ctx, cancel := context.WithCancel(rs.ctx)
	req := request{ID: rs.next, Owner: owner, Resource: r, Policy: policy, ctx: ctx, cancel: cancel}
	rs.active[owner] = req
	return req
}
func (rs *requests) owns(req request) bool {
	current, ok := rs.active[req.Owner]
	return ok && current.ID == req.ID
}
func (rs *requests) stop(owner string) {
	if req, ok := rs.active[owner]; ok {
		req.cancel()
		delete(rs.active, owner)
	}
}
func (rs *requests) finish(req request) {
	if rs.owns(req) {
		rs.stop(req.Owner)
	}
}

func viewOwner(r catalog.Resource) string { return "view:" + r.Key() }
func syncOwner(r catalog.Resource) string { return "sync:" + r.Key() }

func (m *Model) loadResource(r catalog.Resource, policy catalog.Policy, background bool) tea.Cmd {
	owner := viewOwner(r)
	if background {
		owner = syncOwner(r)
	}
	if old, ok := m.requests.active[owner]; ok && policy != catalog.Refresh && old.Resource.Version == r.Version {
		return nil
	}
	req := m.requests.begin(owner, r, policy)
	m.resources[r.Key()] = r
	for i := 0; i < m.ColumnStack.Len(); i++ {
		col := m.ColumnStack.Get(i)
		if col.ContentID() != r.Key() {
			continue
		}
		if col.HasContent() {
			col.SetRefreshing(true)
		} else {
			col.SetLoading(true)
		}
	}
	if id := libraryStateID(r); id != "" {
		state := m.LibraryStates[id]
		state.Status = components.StatusSyncing
		state.Error = nil
		m.LibraryStates[id] = state
		m.updateLibraryStates()
	}
	return LoadResourceCmd(m.Catalog, req)
}

func libraryStateID(r catalog.Resource) string {
	switch r.Kind {
	case catalog.Movies, catalog.Shows, catalog.Mixed:
		return r.LibraryID
	case catalog.Playlists:
		return playlistsLibraryID
	default:
		return ""
	}
}

func (m *Model) topResource() (catalog.Resource, bool) {
	if col := m.ColumnStack.Top(); col != nil {
		r, ok := m.resources[col.ContentID()]
		return r, ok
	}
	return catalog.Resource{}, false
}

func (m *Model) clearLibraryStatus(id string, revision uint64) tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return ClearLibraryStatusMsg{LibraryID: id, Revision: revision} })
}
