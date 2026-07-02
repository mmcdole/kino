package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// NoticeKind classifies footer notifications. The kind decides styling and
// lifetime — call sites never pick durations.
type NoticeKind int

const (
	NoticeInfo    NoticeKind = iota // neutral event ("Launching: ...")
	NoticeSuccess                   // completed action ("Marked watched: ...")
	NoticeError                     // failed action; longer lifetime for reading
	NoticeAlert                     // needs the user: persists until superseded by another alert or dismissed with Esc
)

// Notice is the single footer notification slot.
type Notice struct {
	Text string
	Kind NoticeKind
	Seq  int
}

const (
	noticeDurationShort = 3 * time.Second // info & success
	noticeDurationLong  = 6 * time.Second // errors
)

// ClearNoticeMsg expires the notice with the matching sequence number.
// Carrying the sequence means a stale timer from an earlier notice can never
// clear a newer one.
type ClearNoticeMsg struct{ Seq int }

// notify posts a footer notification and returns its expiry timer command.
// Alerts return nil (no timer) and cannot be displaced by non-alert notices.
func (m *Model) notify(kind NoticeKind, text string) tea.Cmd {
	if m.notice.Kind == NoticeAlert && m.notice.Text != "" && kind != NoticeAlert {
		// An active alert outranks transient notices; drop them rather than
		// hide an actionable message
		return nil
	}

	m.noticeSeq++
	m.notice = Notice{Text: text, Kind: kind, Seq: m.noticeSeq}

	if kind == NoticeAlert {
		return nil
	}

	duration := noticeDurationShort
	if kind == NoticeError {
		duration = noticeDurationLong
	}
	seq := m.noticeSeq
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return ClearNoticeMsg{Seq: seq}
	})
}

// clearNotice removes the current notice unconditionally (explicit dismiss).
func (m *Model) clearNotice() {
	m.notice = Notice{}
}

// expireNotice clears the notice only if seq still identifies it.
func (m *Model) expireNotice(seq int) {
	if m.notice.Seq == seq {
		m.clearNotice()
	}
}
