package tui

import (
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/domain"
)

func TestModalDismissalAndReplacementRejectLateResponses(t *testing.T) {
	m := testModel(t)
	old := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	m.playlistModal.BeginLoading(&domain.MediaItem{ID: "old"})
	m.overlay = overlayPlaylists
	m.cancelPendingModal()
	m = updateModel(m, playlistModalDataMsg{Request: old, Item: domain.MediaItem{ID: "old"}})
	if m.overlay == overlayPlaylists {
		t.Fatal("dismissed modal reopened")
	}
	newer := m.requests.begin("playlist-modal", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, playlistModalDataMsg{Request: newer, Item: domain.MediaItem{ID: "new"}})
	m = updateModel(m, playlistModalDataMsg{Request: old, Item: domain.MediaItem{ID: "old"}})
	if m.playlistModal.Item().ID != "new" {
		t.Fatal("late response reset active modal")
	}
}
