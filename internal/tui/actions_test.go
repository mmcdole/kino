package tui

import (
	"errors"
	"testing"

	"github.com/mmcdole/kino/internal/catalog"
)

func TestUnrelatedActionErrorLeavesColumnLoading(t *testing.T) {
	m := testModel(t)
	r := catalog.LibraryResource(m.libraries[0])
	m.pushColumn(r, "A")
	req := m.requests.begin("playback:x", catalog.Resource{}, catalog.Browse)
	m = updateModel(m, actionMsg{Request: req, Playback: true, Err: errors.New("player unavailable")})
	if !m.columns.Top().IsLoading() {
		t.Fatal("player error stopped column loading")
	}
}

func TestPlaylistRemovalsForDifferentItemsAreIndependent(t *testing.T) {
	m := testModel(t)
	first := m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})
	second := m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: "p", ItemID: "b"})
	if first == nil || second == nil {
		t.Fatal("a pending removal blocked a different item")
	}
	if m.requests.active[mutationOwner(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})].ID == 0 {
		t.Fatal("first removal is not pending")
	}
	m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})
	if m.notice.Text == "" {
		t.Fatal("duplicate removal was ignored silently")
	}
}

func TestPlaylistAddAndRemoveOfOneItemCannotReorder(t *testing.T) {
	m := testModel(t)
	m.beginMutation(catalog.Mutation{Kind: catalog.MutationAddToPlaylist, PlaylistID: "p", ItemID: "a"})
	m.beginMutation(catalog.Mutation{Kind: catalog.MutationRemoveFromPlaylist, PlaylistID: "p", ItemID: "a"})
	if m.notice.Text == "" {
		t.Fatal("a removal started while the add of the same item was pending")
	}
}
