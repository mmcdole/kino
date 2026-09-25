package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// noticeKind classifies footer notifications. The kind decides styling and
// lifetime — call sites never pick durations.
type noticeKind int

const (
	noticeInfo    noticeKind = iota // neutral event ("Launching: ...")
	noticeSuccess                   // completed action ("Marked watched: ...")
	noticeError                     // failed action; longer lifetime for reading
	noticeAlert                     // needs the user: persists until superseded by another alert or dismissed with Esc
)

// notice is the single footer notification slot.
type notice struct {
	Text string
	Kind noticeKind
	Seq  int
}

const (
	noticeDurationShort = 3 * time.Second // info & success
	noticeDurationLong  = 6 * time.Second // errors
)

// clearNoticeMsg expires the notice with the matching sequence number.
// Carrying the sequence means a stale timer from an earlier notice can never
// clear a newer one.
type clearNoticeMsg struct{ Seq int }

// notify posts a footer notification and returns its expiry timer command.
// Alerts return nil (no timer) and cannot be displaced by non-alert notices.
func (m *Model) notify(kind noticeKind, text string) tea.Cmd {
	if m.notice.Kind == noticeAlert && m.notice.Text != "" && kind != noticeAlert {
		// An active alert outranks transient notices; drop them rather than
		// hide an actionable message
		return nil
	}

	m.noticeSeq++
	m.notice = notice{Text: text, Kind: kind, Seq: m.noticeSeq}

	if kind == noticeAlert {
		return nil
	}

	duration := noticeDurationShort
	if kind == noticeError {
		duration = noticeDurationLong
	}
	seq := m.noticeSeq
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return clearNoticeMsg{Seq: seq}
	})
}

// clearNotice removes the current notice unconditionally (explicit dismiss).
func (m *Model) clearNotice() {
	m.notice = notice{}
}

// expireNotice clears the notice only if seq still identifies it.
func (m *Model) expireNotice(seq int) {
	if m.notice.Seq == seq {
		m.clearNotice()
	}
}
