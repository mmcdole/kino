package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
)

// LoadResourceCmd asks the catalog to load a collection. Its content and
// progress arrive through the catalog's published state; the result message
// carries only what the request itself needs to report.
func LoadResourceCmd(svc Catalog, req request) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := svc.Load(req.ctx, req.Resource, req.Policy)
		return LoadDoneMsg{Request: req, Warning: snapshot.Warning, Err: err}
	}
}

// listen waits for the catalog's next published states.
func listen(svc Catalog, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		states, err := svc.Updates(ctx)
		if err != nil {
			return nil
		}
		return StatesMsg(states)
	}
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
