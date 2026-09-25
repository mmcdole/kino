package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/media"
)

func TestNotifySetsNoticeAndTimer(t *testing.T) {
	m := &Model{}

	cmd := m.notify(noticeInfo, "hello")
	if m.notice.Text != "hello" || m.notice.Kind != noticeInfo {
		t.Fatalf("notice not set: %+v", m.notice)
	}
	if cmd == nil {
		t.Fatal("transient notice must return an expiry timer")
	}

	if cmd := m.notify(noticeAlert, "act now"); cmd != nil {
		t.Fatal("alerts must not have expiry timers")
	}
}

// Notice expiry is scoped to the sequence that scheduled it.
func TestStaleTimerCannotClearNewerNotice(t *testing.T) {
	m := &Model{}

	m.notify(noticeInfo, "first")
	firstSeq := m.notice.Seq

	m.notify(noticeSuccess, "second")

	m.expireNotice(firstSeq) // first notice's timer fires late
	if m.notice.Text != "second" {
		t.Fatalf("stale timer cleared newer notice: %+v", m.notice)
	}

	m.expireNotice(m.notice.Seq) // the right timer clears it
	if m.notice.Text != "" {
		t.Fatalf("matching timer failed to clear: %+v", m.notice)
	}
}

func TestAlertOutranksTransientNotices(t *testing.T) {
	m := &Model{}

	m.notify(noticeAlert, "session expired")

	if cmd := m.notify(noticeSuccess, "Marked watched"); cmd != nil {
		t.Fatal("suppressed notice must not schedule a timer")
	}
	if m.notice.Text != "session expired" {
		t.Fatalf("transient notice displaced an alert: %+v", m.notice)
	}

	// Another alert may replace it
	m.notify(noticeAlert, "library gone")
	if m.notice.Text != "library gone" {
		t.Fatalf("alert failed to replace alert: %+v", m.notice)
	}

	// Explicit dismiss always works
	m.clearNotice()
	if m.notice.Text != "" {
		t.Fatal("clearNotice failed")
	}
}

func TestAllOperationErrorsUseAuthenticationAlert(t *testing.T) {
	for _, kind := range []string{"browse", "mutation", "playback", "modal"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t)
			req := m.requests.begin(kind, catalog.Resource{}, catalog.Browse)
			var msg tea.Msg
			switch kind {
			case "browse":
				msg = loadDoneMsg{Request: req, Err: media.ErrAuthFailed}
			case "modal":
				msg = playlistModalDataMsg{Request: req, Err: media.ErrAuthFailed}
			default:
				msg = actionMsg{Request: req, Playback: kind == "playback", Err: media.ErrAuthFailed}
			}
			m = updateModel(m, msg)
			if m.notice.Kind != noticeAlert || m.notice.Text != authFailedStatusMsg {
				t.Fatalf("%s bypassed error classification: %+v", kind, m.notice)
			}
		})
	}
}
