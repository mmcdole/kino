package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeSession struct{ err error }

func (s fakeSession) Logout() error { return s.err }

func TestLogoutUsesSessionAndReportsFailure(t *testing.T) {
	m := testModel(t)
	m.session = fakeSession{err: errors.New("read-only config")}
	m.handleLogout()
	cmd := m.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updateModel(m, cmd())
	if m.LoggedOut || m.loggingOut || m.notice.Kind != noticeError {
		t.Fatalf("failed logout was not reported: %+v", m.notice)
	}

	m.session = fakeSession{}
	m.handleLogout()
	cmd = m.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updateModel(m, cmd())
	if !m.LoggedOut {
		t.Fatal("successful logout did not end the session")
	}
}
