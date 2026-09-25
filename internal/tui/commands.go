package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
)

// loadCmd asks the catalog to load a collection. Its content and
// progress arrive through the catalog's published state; the result message
// carries only what the request itself needs to report.
func loadCmd(svc Catalog, req request) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := svc.Load(req.ctx, req.Resource, req.Policy)
		return loadDoneMsg{Request: req, Warning: snapshot.Warning, Err: err}
	}
}

// listen waits for the catalog's next published states.
func listen(svc Catalog, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		states, err := svc.Updates(ctx)
		if err != nil {
			return nil
		}
		return statesMsg(states)
	}
}

// mutateCmd applies a change to the server and reconciles the cache.
func mutateCmd(svc Catalog, req request, mutation catalog.Mutation) tea.Cmd {
	return func() tea.Msg {
		change, err := svc.Mutate(req.ctx, mutation)
		return actionMsg{Request: req, Change: change, Err: err}
	}
}

// playCmd resolves an item's stream and hands it to the player.
func playCmd(svc Playback, req request, item domain.MediaItem, resume bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if resume {
			err = svc.Resume(req.ctx, item)
		} else {
			err = svc.Play(req.ctx, item)
		}
		return actionMsg{Request: req, Item: item, Playback: true, Err: err}
	}
}

// playlistMembershipCmd loads the playlists and which of them hold item.
func playlistMembershipCmd(svc Catalog, req request, item domain.MediaItem) tea.Cmd {
	return func() tea.Msg {
		membership, err := svc.PlaylistMembership(req.ctx, item.ID)
		return playlistModalDataMsg{Request: req, Membership: membership, Item: item, Err: err}
	}
}

// tick advances spinner animation by one frame.
func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// logoutCmd ends the signed-in session.
func logoutCmd(session Session) tea.Cmd {
	return func() tea.Msg { return logoutCompleteMsg{Error: session.Logout()} }
}
