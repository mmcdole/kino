package tui

import (
	"context"

	"github.com/mmcdole/kino/internal/catalog"
)

type request struct {
	ID       uint64
	Owner    string
	Resource catalog.Resource
	Policy   catalog.Policy
	ctx      context.Context
	cancel   context.CancelFunc
}

// requests is owned by the event loop. A request's context is its
// subscription: canceling a view detaches it from shared catalog work.
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
