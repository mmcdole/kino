package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
)

// LoadResourceCmd adapts the application's result stream to Bubble Tea. Cached
// data and progress can coalesce; the terminal result has its own buffered slot
// and cannot be dropped or strand a producer after navigation cancels a request.
func LoadResourceCmd(svc Catalog, req request) tea.Cmd {
	return func() tea.Msg {
		cached := make(chan ResourceMsg, 1)
		progress := make(chan ResourceMsg, 1)
		network := make(chan ResourceMsg, 1)
		done := make(chan ResourceMsg, 1)
		go func() {
			snapshot, err := svc.Load(req.ctx, req.Resource, req.Policy, catalog.Observer{
				Network: func() {
					select {
					case network <- ResourceMsg{Request: req, Stage: loadNetwork}:
					default:
					}
				},
				Cached: func(snapshot catalog.Snapshot) {
					select {
					case cached <- ResourceMsg{Request: req, Stage: loadCached, Snapshot: snapshot}:
					default:
					}
				},
				Progress: func(p catalog.Progress) {
					select {
					case progress <- ResourceMsg{Request: req, Stage: loadProgress, Progress: p}:
					default:
					}
				},
			})
			done <- ResourceMsg{Request: req, Stage: loadFinished, Snapshot: snapshot, Err: err}
		}()
		return readResource(req, cached, progress, network, done)
	}
}
func readResource(req request, cached, progress, network, done <-chan ResourceMsg) tea.Msg {
	var msg ResourceMsg
	select {
	case msg = <-done:
		return msg
	default:
	}
	select {
	case msg = <-cached:
	case msg = <-progress:
	case msg = <-network:
	case msg = <-done:
		return msg
	case <-req.ctx.Done():
		return ResourceMsg{Request: req, Stage: loadFinished, Err: req.ctx.Err()}
	}
	msg.Next = func() tea.Msg { return readResource(req, cached, progress, network, done) }
	return msg
}

func MutationCmd(svc Catalog, req request, mutation catalog.Mutation) tea.Cmd {
	return func() tea.Msg {
		change, err := svc.Mutate(req.ctx, mutation)
		return ActionMsg{Request: req, Change: change, Err: err}
	}
}
func PlayItemCmd(svc Playback, req request, item domain.MediaItem, resume bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if resume {
			err = svc.Resume(req.ctx, item)
		} else {
			err = svc.Play(req.ctx, item)
		}
		return ActionMsg{Request: req, Item: item, Playback: true, Err: err}
	}
}
func LoadPlaylistModalDataCmd(svc Catalog, req request, item domain.MediaItem) tea.Cmd {
	return func() tea.Msg {
		membership, err := svc.PlaylistMembership(req.ctx, item.ID)
		return PlaylistModalDataMsg{Request: req, Membership: membership, Item: item, Err: err}
	}
}
func TickCmd(delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return TickMsg{} })
}
func LogoutCmd() tea.Cmd {
	return func() tea.Msg { return LogoutCompleteMsg{Error: config.ClearServerConfig()} }
}
