package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
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
	ID               uint64
	Owner            string
	Resource         catalog.Resource
	Policy           catalog.Policy
	Revision         uint64
	Network          bool
	IndicatorVisible bool
	Progress         catalog.Progress
	ctx              context.Context
	cancel           context.CancelFunc
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
	req.Revision = m.collection(r).RequiredRevision
	m.requests.active[owner] = req
	m.collection(r).Resource = r
	m.updateResourceFeedback(r)
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
		r, ok := m.resource(col.ContentID())
		return r, ok
	}
	return catalog.Resource{}, false
}
