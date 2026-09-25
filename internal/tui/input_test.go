package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/domain"
)

func TestOverlayReceivesKeysBeforeColumnsAndGlobalKeys(t *testing.T) {
	m := testModel(t)
	m.handleGlobalSearch()
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.overlay != overlaySearch || m.globalSearch.Query() != "q" {
		t.Fatal("q quit or left search instead of typing into it")
	}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone {
		t.Fatal("esc did not close search")
	}

	m.overlay, m.confirmDelete = overlayConfirmDelete, &domain.Playlist{ID: "p", Title: "P"}
	m = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone || m.confirmDelete != nil || m.columns.Len() != 1 {
		t.Fatal("esc on a confirmation did more than cancel it")
	}
}
