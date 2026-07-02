package tui

import "testing"

func TestNotifySetsNoticeAndTimer(t *testing.T) {
	m := &Model{}

	cmd := m.notify(NoticeInfo, "hello")
	if m.notice.Text != "hello" || m.notice.Kind != NoticeInfo {
		t.Fatalf("notice not set: %+v", m.notice)
	}
	if cmd == nil {
		t.Fatal("transient notice must return an expiry timer")
	}

	if cmd := m.notify(NoticeAlert, "act now"); cmd != nil {
		t.Fatal("alerts must not have expiry timers")
	}
}

// A stale timer from an earlier notice must not clear a newer one — this was
// the bug class where "Marked watched"'s timer deleted the persistent auth
// alert posted a second later.
func TestStaleTimerCannotClearNewerNotice(t *testing.T) {
	m := &Model{}

	m.notify(NoticeInfo, "first")
	firstSeq := m.notice.Seq

	m.notify(NoticeSuccess, "second")

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

	m.notify(NoticeAlert, "session expired")

	if cmd := m.notify(NoticeSuccess, "Marked watched"); cmd != nil {
		t.Fatal("suppressed notice must not schedule a timer")
	}
	if m.notice.Text != "session expired" {
		t.Fatalf("transient notice displaced an alert: %+v", m.notice)
	}

	// Another alert may replace it
	m.notify(NoticeAlert, "library gone")
	if m.notice.Text != "library gone" {
		t.Fatalf("alert failed to replace alert: %+v", m.notice)
	}

	// Explicit dismiss always works
	m.clearNotice()
	if m.notice.Text != "" {
		t.Fatal("clearNotice failed")
	}
}
