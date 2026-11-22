package player

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// Tracker reports on plays the server keeps (a Stream with a PlayID). A
// server that keeps none doesn't implement it.
type Tracker interface {
	ReportPlay(ctx context.Context, playID string, p media.Progress) error
	StopPlay(ctx context.Context, playID string, at time.Duration) error
	// FallbackPlay ends a play the player failed to open (detail is the
	// player's own error) and starts one the server transcodes;
	// media.ErrNoFallback when there is nothing else to try.
	FallbackPlay(ctx context.Context, playID string, at time.Duration, detail string) (media.Stream, error)
}

const (
	heartbeatInterval = 10 * time.Second
	ipcConnectTimeout = 10 * time.Second
	trackerTimeout    = 10 * time.Second
)

// session follows one tracked play from the player's launch to its exit:
// heartbeats while it plays, one fallback if it fails before its first
// frame, and a stop with the last position.
type session struct {
	tracker Tracker
	start   func(context.Context, launchSpec) (running, error)
	logger  *slog.Logger
	ipcDir  string
	ticker  func(time.Duration) (<-chan time.Time, func())
	now     func() time.Time
}

// maxSocketPath is the longest unix socket path every platform takes (macOS
// 104 bytes, Linux 108); a longer one fails in the player, not here.
const maxSocketPath = 100

// socketDir returns the first private-enough directory whose socket paths
// fit.
func socketDir() string {
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir != "" && len(filepath.Join(dir, socketName(1<<22, 9))) <= maxSocketPath {
			return dir
		}
	}
	return "/tmp"
}

func socketName(pid, launch int) string { return fmt.Sprintf("kino-%d-%d.sock", pid, launch) }

func newSession(tracker Tracker, l *launcher, logger *slog.Logger) *session {
	return &session{
		tracker: tracker,
		start:   l.start,
		logger:  logger,
		ipcDir:  socketDir(),
		ticker: func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		},
		now: time.Now,
	}
}

// outcome is how following a player ended.
type outcome struct {
	position time.Duration
	failed   bool   // the player gave up before showing a frame
	detail   string // the player's error, when it failed
	gone     bool   // the server no longer has the play
	detached bool   // the player's lifetime can't be followed
}

// run launches stream and follows it until the player exits or ctx ends.
// ready is called once: with nil when the play is playing (or the session
// can't tell), else with what ended it first, such as a refused fallback.
func (s *session) run(ctx context.Context, stream media.Stream, ready func(error)) (err error) {
	var once sync.Once
	signal := func(err error) { once.Do(func() { ready(err) }) }
	defer func() { signal(err) }()
	playing := func() { signal(nil) }

	fellBack := false
	for launch := 0; ; launch++ {
		part, offset := partAt(stream)
		sock := filepath.Join(s.ipcDir, socketName(os.Getpid(), launch))
		_ = os.Remove(sock)
		spec := launchSpec{urls: partURLs(stream)[part:], offset: offset, ipc: sock}
		p, err := s.start(ctx, spec)
		if err != nil {
			s.stop(ctx, stream.PlayID, stream.Start)
			return err
		}
		out := s.follow(ctx, stream, part, p, sock, playing)
		_ = os.Remove(sock)

		switch {
		case out.failed && !fellBack && !out.gone:
			s.logger.Warn("player failed before its first frame, asking the server for a transcode")
			next, err := s.fallback(ctx, stream.PlayID, out.position, out.detail)
			if err != nil {
				return err
			}
			stream, fellBack = next, true
			continue
		case out.gone, out.detached:
			return nil
		default:
			s.stop(ctx, stream.PlayID, out.position)
			if out.failed {
				return fmt.Errorf("player failed to open the stream")
			}
			return nil
		}
	}
}

// follow reports on a launched player until it exits, calling playing once
// it has a position.
func (s *session) follow(ctx context.Context, stream media.Stream, part int, p running, sock string, playing func()) outcome {
	if !p.ipc {
		playing()
		return s.followBlind(ctx, stream, p)
	}
	dialCtx, cancel := context.WithTimeout(ctx, ipcConnectTimeout)
	conn, err := dialMPV(dialCtx, sock)
	cancel()
	if err != nil {
		s.logger.Warn("player IPC unavailable, playing without progress reports", "error", err)
		playing()
		return s.followBlind(ctx, stream, p)
	}
	defer conn.Close()
	go p.wait()

	offsets := partOffsets(stream)
	out := outcome{position: stream.Start}
	var (
		playlistPos int
		paused      bool
		shown       bool      // playback has a position: the file opened and is playing
		seekAt      time.Time // when the seek in progress began
		ended       bool
	)
	report := func(state media.PlayState, seekToFrame time.Duration) {
		if out.gone {
			return
		}
		err := s.report(ctx, stream.PlayID, media.Progress{Position: out.position, State: state, SeekToFirstFrame: seekToFrame})
		if errors.Is(err, media.ErrItemNotFound) {
			// Expired, or stopped by an admin: the player is left alone.
			s.logger.Warn("server ended the play")
			out.gone = true
		}
	}
	state := func() media.PlayState {
		if paused {
			return media.PlayPaused
		}
		return media.PlayPlaying
	}

	beat, stopBeat := s.ticker(heartbeatInterval)
	defer stopBeat()
	for {
		select {
		case <-ctx.Done():
			return out
		case <-beat:
			if shown && !ended {
				report(state(), 0)
			}
		case e, ok := <-conn.Events:
			if !ok {
				return out
			}
			switch e.Event {
			case "property-change":
				switch e.Name {
				case "time-pos":
					if secs, ok := e.seconds(); ok {
						i := min(part+playlistPos, len(offsets)-1)
						out.position = offsets[i] + time.Duration(secs*float64(time.Second))
						// Observing sends the current value, so this
						// also covers a first frame shown before the
						// session connected, whose playback-restart it
						// missed.
						if !shown {
							shown = true
							playing()
							report(state(), 0)
						}
					}
				case "pause":
					paused = e.flag()
					if shown {
						report(state(), 0)
					}
				case "playlist-pos":
					if secs, ok := e.seconds(); ok && secs >= 0 {
						playlistPos = int(secs)
					}
				}
			case "seek":
				seekAt = s.now()
			case "playback-restart":
				if !shown {
					shown = true
					playing()
					report(state(), 0)
				} else if !seekAt.IsZero() {
					report(state(), s.now().Sub(seekAt))
				}
				seekAt = time.Time{}
			case "end-file":
				switch {
				case e.Reason == "error" && !shown:
					out.failed, out.detail = true, e.FileError
				case e.Reason == "eof" && part+playlistPos >= len(offsets)-1:
					ended = true
					report(media.PlayEnded, 0)
				}
			}
		}
	}
}

// followBlind keeps a play alive for a player that can't say where it is. Its
// heartbeats repeat the start, so the resume point stays where it was.
func (s *session) followBlind(ctx context.Context, stream media.Stream, p running) outcome {
	out := outcome{position: stream.Start}
	if p.cmd == nil {
		s.logger.Info("player's lifetime can't be followed; the play ends on its own once it stops fetching")
		out.detached = true
		return out
	}
	s.logger.Info("player has no IPC; resume points need mpv")
	exited := make(chan struct{})
	go func() { p.wait(); close(exited) }()
	beat, stopBeat := s.ticker(heartbeatInterval)
	defer stopBeat()
	for {
		select {
		case <-ctx.Done():
			return out
		case <-exited:
			return out
		case <-beat:
			err := s.report(ctx, stream.PlayID, media.Progress{Position: stream.Start, State: media.PlayPlaying})
			if errors.Is(err, media.ErrItemNotFound) {
				out.gone = true
				return out
			}
		}
	}
}

// The tracker calls outlive ctx: a session canceled by a signal still stops
// its play.
func (s *session) report(ctx context.Context, playID string, p media.Progress) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trackerTimeout)
	defer cancel()
	err := s.tracker.ReportPlay(ctx, playID, p)
	if err != nil && !errors.Is(err, media.ErrItemNotFound) {
		s.logger.Warn("heartbeat failed", "error", err)
	}
	return err
}

func (s *session) stop(ctx context.Context, playID string, at time.Duration) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trackerTimeout)
	defer cancel()
	if err := s.tracker.StopPlay(ctx, playID, at); err != nil && !errors.Is(err, media.ErrItemNotFound) {
		s.logger.Warn("stopping play failed", "error", err)
	}
}

func (s *session) fallback(ctx context.Context, playID string, at time.Duration, detail string) (media.Stream, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), trackerTimeout)
	defer cancel()
	return s.tracker.FallbackPlay(ctx, playID, at, detail)
}

func partURLs(stream media.Stream) []string {
	urls := make([]string, len(stream.Parts))
	for i, p := range stream.Parts {
		urls[i] = p.URL
	}
	return urls
}

// partOffsets returns where each part begins on the joined timeline.
func partOffsets(stream media.Stream) []time.Duration {
	offsets := make([]time.Duration, len(stream.Parts))
	for i := 1; i < len(stream.Parts); i++ {
		offsets[i] = offsets[i-1] + stream.Parts[i-1].Duration
	}
	return offsets
}

// partAt returns the part holding the stream's start and the offset into it.
func partAt(stream media.Stream) (int, time.Duration) {
	offsets := partOffsets(stream)
	for i := len(offsets) - 1; i > 0; i-- {
		if stream.Start >= offsets[i] {
			return i, stream.Start - offsets[i]
		}
	}
	return 0, stream.Start
}
