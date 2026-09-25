package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
)

// mutationOwner identifies what a write changes. Adding and removing the same
// item in a playlist share an owner so they cannot be reordered; writes to
// different items are independent, and the catalog serializes them.
func mutationOwner(change catalog.Mutation) string {
	switch change.Kind {
	case catalog.MutationWatch:
		return "mutation:watch:" + change.ItemID
	case catalog.MutationCreatePlaylist:
		return "mutation:create:" + change.Title
	case catalog.MutationDeletePlaylist:
		return "mutation:playlist:" + change.PlaylistID
	default:
		return "mutation:playlist:" + change.PlaylistID + ":" + change.ItemID
	}
}

func (m *Model) beginMutation(change catalog.Mutation) tea.Cmd {
	owner := mutationOwner(change)
	// A second keypress cannot reorder two writes to the same item. Keep the
	// first operation pending until its result, rather than launching duplicates.
	if _, pending := m.requests.active[owner]; pending {
		return m.notify(noticeInfo, "Already updating — waiting for the server")
	}
	req := m.requests.begin(owner, catalog.Resource{}, catalog.Browse)
	return mutateCmd(m.catalog, req, change)
}

func (m *Model) beginPlayback(item media.Item, resume bool) tea.Cmd {
	owner := "playback:" + item.ID
	if _, pending := m.requests.active[owner]; pending {
		return nil
	}
	req := m.requests.begin(owner, catalog.Resource{}, catalog.Browse)
	return playCmd(m.playback, req, item, resume)
}

func (m *Model) handleAction(msg actionMsg) tea.Cmd {
	if !m.requests.owns(msg.Request) {
		return nil
	}
	m.requests.finish(msg.Request)
	if msg.Playback {
		if msg.Err != nil {
			return m.notifyError("Starting playback", msg.Err)
		}
		return m.notify(noticeSuccess, "Launched: "+msg.Item.Title)
	}
	change := msg.Change
	var cmds []tea.Cmd
	for _, r := range change.Resources {
		if _, tracked := m.collections[r.Key()]; !tracked && r.Kind != catalog.Playlists {
			continue
		}
		if r.LibraryID != "" && m.findLibrary(r.LibraryID) == nil {
			continue
		}
		// Reconciled snapshots arrive as published state. Collections the write
		// may have changed on the server revalidate if anything shows them.
		background := libraryStateID(r) != ""
		visible := false
		for i := 0; i < m.columns.Len(); i++ {
			visible = visible || m.columns.Get(i).ContentID() == r.Key()
		}
		if background || visible {
			cmds = append(cmds, m.loadResource(r, catalog.Revalidate, background))
		}
	}

	if change.Applied && change.Mutation.Kind == catalog.MutationDeletePlaylist {
		if r, ok := m.topResource(); ok && r.Kind == catalog.PlaylistItems && r.ID == change.Mutation.PlaylistID {
			cmds = append(cmds, m.handleBack())
		}
	}
	if msg.Err != nil {
		cmds = append(cmds, m.notifyError("Update failed", msg.Err))
	} else {
		text := "Playlist updated"
		if change.Mutation.Kind == catalog.MutationWatch {
			text = "Marked unwatched: " + change.Mutation.Title
			if change.Mutation.Played {
				text = "Marked watched: " + change.Mutation.Title
			}
		}
		if change.Mutation.Kind == catalog.MutationCreatePlaylist {
			text = "Created playlist: " + change.Mutation.Title
		}
		if change.Mutation.Kind == catalog.MutationDeletePlaylist {
			text = "Playlist deleted"
		}
		cmds = append(cmds, m.notify(noticeSuccess, text))
	}
	if change.Warning != nil {
		cmds = append(cmds, m.notifyError("Server updated; local cache needs refresh", change.Warning))
	}
	return tea.Batch(cmds...)
}
