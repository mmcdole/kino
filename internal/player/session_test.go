package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// call is one request a session made of the server.
type call struct {
	kind     string // report, stop or fallback
	play     string
	progress media.Progress
	at       time.Duration
	detail   string
}

type fakeTracker struct {
	calls    chan call
	report   func(call) error
	fallback func(call) (media.Stream, error)
}

func newFakeTracker() *fakeTracker { return &fakeTracker{calls: make(chan call, 32)} }

func (f *fakeTracker) ReportPlay(_ context.Context, id string, p media.Progress) error {
	c := call{kind: "report", play: id, progress: p}
	f.calls <- c
	if f.report != nil {
		return f.report(c)
	}
	return nil
}

func (f *fakeTracker) StopPlay(_ context.Context, id string, at time.Duration) error {
	f.calls <- call{kind: "stop", play: id, at: at}
	return nil
}

func (f *fakeTracker) FallbackPlay(_ context.Context, id string, at time.Duration, detail string) (media.Stream, error) {
	c := call{kind: "fallback", play: id, at: at, detail: detail}
	f.calls <- c
	return f.fallback(c)
}

func (f *fakeTracker) next(t *testing.T) call {
	t.Helper()
	select {
	case c := <-f.calls:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("no call from the session")
		return call{}
	}
}

func (f *fakeTracker) none(t *testing.T) {
	t.Helper()
	select {
	case c := <-f.calls:
		t.Fatalf("unexpected call %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}

// fakeMPV is a player listening on the socket a launch asks for. Each launch
// is handed to the test, which scripts the player's events.
type fakeMPV struct {
	spec   launchSpec
	conn   net.Conn
	sawObs chan struct{}
}

func (m *fakeMPV) send(t *testing.T, event map[string]any) {
	t.Helper()
	line, _ := json.Marshal(event)
	if _, err := m.conn.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (m *fakeMPV) prop(t *testing.T, name string, value any) {
	m.send(t, map[string]any{"event": "property-change", "name": name, "data": value})
}

func (m *fakeMPV) quit() { m.conn.Close() }

type sessionRig struct {
	tracker  *fakeTracker
	launches chan *fakeMPV
	beat     chan time.Time
	clock    atomic.Int64 // unix nanoseconds
	done     chan error
	ready    chan error
}

func newRig(t *testing.T, ipc bool) (*sessionRig, *session) {
	t.Helper()
	rig := &sessionRig{
		tracker:  newFakeTracker(),
		launches: make(chan *fakeMPV, 2),
		beat:     make(chan time.Time),
		done:     make(chan error, 1),
		ready:    make(chan error, 1),
	}
	s := &session{
		tracker: rig.tracker,
		logger:  slog.New(slog.DiscardHandler),
		ipcDir:  t.TempDir(),
		ticker:  func(time.Duration) (<-chan time.Time, func()) { return rig.beat, func() {} },
		now:     func() time.Time { return time.Unix(0, rig.clock.Load()) },
	}
	s.start = func(ctx context.Context, spec launchSpec) (running, error) {
		if !ipc {
			cmd := exec.Command("sleep", "60")
			if err := cmd.Start(); err != nil {
				return running{}, err
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			rig.launches <- &fakeMPV{spec: spec, conn: &procConn{cmd: cmd}}
			return running{cmd: cmd}, nil
		}
		ln, err := net.Listen("unix", spec.ipc)
		if err != nil {
			return running{}, err
		}
		m := &fakeMPV{spec: spec, sawObs: make(chan struct{})}
		go func() {
			defer ln.Close()
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			m.conn = conn
			r := bufio.NewReader(conn)
			for range mpvProperties {
				_, _ = r.ReadString('\n')
			}
			close(m.sawObs)
			rig.launches <- m
		}()
		return running{ipc: true}, nil
	}
	return rig, s
}

func (r *sessionRig) run(s *session, ctx context.Context, stream media.Stream) {
	go func() { r.done <- s.run(ctx, stream, func(err error) { r.ready <- err }) }()
}

func (r *sessionRig) launch(t *testing.T) *fakeMPV {
	t.Helper()
	select {
	case m := <-r.launches:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("player never launched")
		return nil
	}
}

func (r *sessionRig) finish(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("session never ended")
		return nil
	}
}

// procConn lets a blind test end its player like an IPC one.
type procConn struct {
	net.Conn
	cmd *exec.Cmd
}

func (p *procConn) Close() error { return p.cmd.Process.Kill() }

// settle makes sure the session has taken every event sent so far: a pause
// and resume each report at once, in order with the events before them.
func (r *sessionRig) settle(t *testing.T, m *fakeMPV) {
	t.Helper()
	m.prop(t, "pause", true)
	r.tracker.next(t)
	m.prop(t, "pause", false)
	r.tracker.next(t)
}

func oneFile(id string) media.Stream {
	return media.Stream{PlayID: id, Parts: []media.StreamPart{{URL: "http://kivo/" + id, Duration: time.Hour}}, Start: 10 * time.Minute}
}

func expect(t *testing.T, c call, kind string, pos time.Duration, state media.PlayState) {
	t.Helper()
	got := c.progress.Position
	if kind != "report" {
		got = c.at
	}
	if c.kind != kind || got != pos || (kind == "report" && c.progress.State != state) {
		t.Fatalf("got %+v, want %s at %v %s", c, kind, pos, state)
	}
}

// A normal play: a report at the first frame, on pause, each heartbeat and
// after a seek (with the time to its first frame), then the end and a stop
// with the last position.
func TestSessionReportsUntilPlayerExits(t *testing.T) {
	rig, s := newRig(t, true)
	rig.run(s, context.Background(), oneFile("p1"))
	m := rig.launch(t)
	if m.spec.offset != 10*time.Minute || m.spec.urls[0] != "http://kivo/p1" {
		t.Fatalf("launched %+v", m.spec)
	}

	select {
	case err := <-rig.ready:
		t.Fatalf("ready (%v) before the play had a position", err)
	default:
	}
	m.prop(t, "time-pos", 600.5)
	m.send(t, map[string]any{"event": "playback-restart"})
	expect(t, rig.tracker.next(t), "report", 600500*time.Millisecond, media.PlayPlaying)
	if err := <-rig.ready; err != nil {
		t.Fatal(err)
	}

	m.prop(t, "pause", true)
	expect(t, rig.tracker.next(t), "report", 600500*time.Millisecond, media.PlayPaused)
	m.prop(t, "pause", false)
	expect(t, rig.tracker.next(t), "report", 600500*time.Millisecond, media.PlayPlaying)

	m.prop(t, "time-pos", 610.0)
	rig.settle(t, m)
	rig.beat <- time.Now()
	expect(t, rig.tracker.next(t), "report", 610*time.Second, media.PlayPlaying)

	m.send(t, map[string]any{"event": "seek"})
	rig.settle(t, m)
	rig.clock.Add(int64(400 * time.Millisecond))
	m.prop(t, "time-pos", 1800.0)
	m.send(t, map[string]any{"event": "playback-restart"})
	c := rig.tracker.next(t)
	expect(t, c, "report", 30*time.Minute, media.PlayPlaying)
	if c.progress.SeekToFirstFrame != 400*time.Millisecond {
		t.Fatalf("seek to first frame = %v", c.progress.SeekToFirstFrame)
	}

	m.prop(t, "time-pos", 3600.0)
	m.send(t, map[string]any{"event": "end-file", "reason": "eof"})
	expect(t, rig.tracker.next(t), "report", time.Hour, media.PlayEnded)
	m.quit()
	expect(t, rig.tracker.next(t), "stop", time.Hour, "")
	if err := rig.finish(t); err != nil {
		t.Fatal(err)
	}
}

// A player that fails before its first frame gets one fallback: the server
// ends that play and the session relaunches the player on the new one.
func TestSessionFallsBackOnce(t *testing.T) {
	rig, s := newRig(t, true)
	rig.tracker.fallback = func(c call) (media.Stream, error) {
		next := oneFile("p2")
		next.Start = c.at
		return next, nil
	}
	rig.run(s, context.Background(), oneFile("p1"))
	m := rig.launch(t)
	m.send(t, map[string]any{"event": "end-file", "reason": "error", "file_error": "loading failed"})
	m.quit()
	c := rig.tracker.next(t)
	if c.kind != "fallback" || c.play != "p1" || c.at != 10*time.Minute || c.detail != "loading failed" {
		t.Fatalf("fallback call = %+v", c)
	}

	m2 := rig.launch(t)
	if m2.spec.urls[0] != "http://kivo/p2" || m2.spec.offset != 10*time.Minute {
		t.Fatalf("relaunched %+v", m2.spec)
	}
	// The transcode fails too: no second fallback, and the play is stopped.
	m2.send(t, map[string]any{"event": "end-file", "reason": "error"})
	m2.quit()
	expect(t, rig.tracker.next(t), "stop", 10*time.Minute, "")
	if err := rig.finish(t); err == nil {
		t.Fatal("a play that failed twice ended without an error")
	}
	if err := <-rig.ready; err == nil {
		t.Fatal("the TUI was told a play that never opened is playing")
	}
}

// With nothing left to try, the session ends with the server's word for it.
func TestSessionNoFallback(t *testing.T) {
	rig, s := newRig(t, true)
	rig.tracker.fallback = func(call) (media.Stream, error) { return media.Stream{}, media.ErrNoFallback }
	rig.run(s, context.Background(), oneFile("p1"))
	m := rig.launch(t)
	m.send(t, map[string]any{"event": "end-file", "reason": "error"})
	m.quit()
	rig.tracker.next(t)
	if err := rig.finish(t); !errors.Is(err, media.ErrNoFallback) {
		t.Fatalf("err = %v", err)
	}
	if err := <-rig.ready; !errors.Is(err, media.ErrNoFallback) {
		t.Fatalf("ready = %v, want the refusal", err)
	}
	rig.tracker.none(t)
}

// A play the server ended (expired, or stopped by an admin) gets no more
// reports and no stop; the player is left alone.
func TestSessionLeavesEndedPlayAlone(t *testing.T) {
	rig, s := newRig(t, true)
	rig.tracker.report = func(call) error { return fmt.Errorf("gone: %w", media.ErrItemNotFound) }
	rig.run(s, context.Background(), oneFile("p1"))
	m := rig.launch(t)
	m.send(t, map[string]any{"event": "playback-restart"})
	rig.tracker.next(t)
	m.prop(t, "pause", true)
	m.quit()
	if err := rig.finish(t); err != nil {
		t.Fatal(err)
	}
	rig.tracker.none(t)
}

// A signal stops the play where it was, though ctx is canceled.
func TestSessionStopsOnSignal(t *testing.T) {
	rig, s := newRig(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	rig.run(s, ctx, oneFile("p1"))
	m := rig.launch(t)
	m.prop(t, "time-pos", 700.0)
	m.send(t, map[string]any{"event": "playback-restart"})
	rig.tracker.next(t)
	cancel()
	expect(t, rig.tracker.next(t), "stop", 700*time.Second, "")
	rig.finish(t)
}

// Positions are on the parts' joined timeline: a start in the second part
// launches from it, and the playlist position moves the base.
func TestSessionMultiPartPositions(t *testing.T) {
	rig, s := newRig(t, true)
	stream := media.Stream{PlayID: "p1", Start: 70 * time.Minute, Parts: []media.StreamPart{
		{URL: "http://kivo/0", Duration: time.Hour},
		{URL: "http://kivo/1", Duration: time.Hour},
		{URL: "http://kivo/2", Duration: time.Hour},
	}}
	rig.run(s, context.Background(), stream)
	m := rig.launch(t)
	if len(m.spec.urls) != 2 || m.spec.urls[0] != "http://kivo/1" || m.spec.offset != 10*time.Minute {
		t.Fatalf("launched %+v", m.spec)
	}
	m.prop(t, "time-pos", 900.0)
	m.send(t, map[string]any{"event": "playback-restart"})
	expect(t, rig.tracker.next(t), "report", 75*time.Minute, media.PlayPlaying)
	m.prop(t, "playlist-pos", 1)
	m.prop(t, "time-pos", 60.0)
	rig.settle(t, m)
	rig.beat <- time.Now()
	expect(t, rig.tracker.next(t), "report", 121*time.Minute, media.PlayPlaying)
	m.quit()
	expect(t, rig.tracker.next(t), "stop", 121*time.Minute, "")
}

// A player without IPC keeps the play alive with heartbeats that repeat the
// start, and stops it there when the player exits.
func TestSessionBlindPlayer(t *testing.T) {
	rig, s := newRig(t, false)
	rig.run(s, context.Background(), oneFile("p1"))
	m := rig.launch(t)
	if err := <-rig.ready; err != nil {
		t.Fatalf("blind player: ready = %v", err)
	}
	rig.beat <- time.Now()
	expect(t, rig.tracker.next(t), "report", 10*time.Minute, media.PlayPlaying)
	m.quit()
	expect(t, rig.tracker.next(t), "stop", 10*time.Minute, "")
	rig.finish(t)
}

// A launch that fails is reported to the TUI, and the play is stopped so it
// doesn't hold a stream slot.
func TestSessionLaunchFailure(t *testing.T) {
	rig, s := newRig(t, true)
	s.start = func(context.Context, launchSpec) (running, error) { return running{}, errors.New("no player") }
	rig.run(s, context.Background(), oneFile("p1"))
	if err := <-rig.ready; err == nil {
		t.Fatal("launch failure not reported")
	}
	expect(t, rig.tracker.next(t), "stop", 10*time.Minute, "")
}

// A runtime directory too long for a unix socket path is passed over: mpv
// can't create the socket, and the play would go unreported.
func TestSocketDirFitsPathLimit(t *testing.T) {
	long := "/tmp/" + strings.Repeat("d", 100)
	t.Setenv("XDG_RUNTIME_DIR", long)
	t.Setenv("TMPDIR", long)
	if dir := socketDir(); dir != "/tmp" {
		t.Fatalf("socket dir = %s", dir)
	}
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if dir := socketDir(); dir != "/run/user/1000" {
		t.Fatalf("socket dir = %s", dir)
	}
}

// A session that connects after mpv showed its first frame never sees that
// playback-restart; the position it's sent on observing still starts the
// reports.
func TestSessionConnectsAfterFirstFrame(t *testing.T) {
	rig, s := newRig(t, true)
	rig.run(s, context.Background(), oneFile("p1"))
	m := rig.launch(t)
	m.prop(t, "time-pos", 640.0)
	expect(t, rig.tracker.next(t), "report", 640*time.Second, media.PlayPlaying)
	m.prop(t, "time-pos", 650.0)
	rig.settle(t, m)
	rig.beat <- time.Now()
	expect(t, rig.tracker.next(t), "report", 650*time.Second, media.PlayPlaying)
	m.quit()
	expect(t, rig.tracker.next(t), "stop", 650*time.Second, "")
}
