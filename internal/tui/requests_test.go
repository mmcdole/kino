package tui

import (
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
)

func TestModalDismissalAndReplacementRejectLateResponses(t *testing.T) {
	m := testModel(t)
	old := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	m.playlistModal.BeginLoading(&media.Item{ID: "old"})
	m.overlay = overlayPlaylists
	m.cancelPendingModal()
	m = updateModel(m, playlistModalDataMsg{Request: old, Item: media.Item{ID: "old"}})
	if m.overlay == overlayPlaylists {
		t.Fatal("dismissed modal reopened")
	}
	newer := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, playlistModalDataMsg{Request: newer, Item: media.Item{ID: "new"}})
	m = updateModel(m, playlistModalDataMsg{Request: old, Item: media.Item{ID: "old"}})
	if m.playlistModal.Item().ID != "new" {
		t.Fatal("late response reset active modal")
	}
}
