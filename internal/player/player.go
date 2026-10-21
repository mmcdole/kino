package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// Starter is the backend operation playback consumes. A backend whose plays
// need reporting also implements Tracker.
type Starter interface {
	StartPlay(ctx context.Context, itemID string, at time.Duration) (media.Stream, error)
}

// SessionCommand is the hidden kino subcommand that runs a play session.
const SessionCommand = "play-session"

// readyWait is how long starting playback waits for the session to say the
// player is playing.
const readyWait = 10 * time.Second

// Player starts an item's play and opens it in an external player.
type Player struct {
	launcher *launcher
	starter  Starter
	logger   *slog.Logger
}

// New returns a Player that launches command with args, or detects an
// installed player when command is empty. seekFlag overrides the known
// resume flag for command (e.g. "--start=%d").
func New(command string, args []string, seekFlag string, starter Starter, logger *slog.Logger) *Player {
	if logger == nil {
		logger = slog.Default()
	}
	return &Player{
		launcher: newLauncher(command, args, seekFlag, logger),
		starter:  starter,
		logger:   logger,
	}
}

// Play starts playback of a media item from the beginning
func (s *Player) Play(ctx context.Context, item media.Item) error {
	return s.playItem(ctx, item, 0)
}

// Resume starts playback from the saved position
func (s *Player) Resume(ctx context.Context, item media.Item) error {
	return s.playItem(ctx, item, item.ViewOffset)
}

// playItem starts the play and launches the player. A play the server keeps
// is handed to a session process, which outlives kino as the player does.
func (s *Player) playItem(ctx context.Context, item media.Item, offset time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stream, err := s.starter.StartPlay(ctx, item.ID, offset)
	if err != nil {
		s.logger.Error("failed to start play", "error", err, "itemID", item.ID)
		return err
	}
	if len(stream.Parts) == 0 {
		return media.ErrItemNotFound
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	s.logger.Info("launching playback", "title", item.Title, "itemID", item.ID, "offset", stream.Start, "tracked", stream.PlayID != "")

	if stream.PlayID == "" {
		return s.launcher.Launch(ctx, stream.Parts[0].URL, stream.Start)
	}
	if err := spawnSession(ctx, stream); err != nil {
		if tracker, ok := s.starter.(Tracker); ok {
			stopCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), trackerTimeout)
			defer stop()
			_ = tracker.StopPlay(stopCtx, stream.PlayID, stream.Start)
		}
		return err
	}
	return nil
}

// spawnSession starts `kino play-session` with stream on its stdin and waits
// for it to report the play playing.
func spawnSession(ctx context.Context, stream media.Stream) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding kino to run the play session: %w", err)
	}
	data, err := json.Marshal(stream)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, SessionCommand)
	cmd.Stdin = strings.NewReader(string(data))
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the play session: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(out).ReadString('\n')
		line <- strings.TrimSpace(l)
	}()
	// The session says once the player plays or failed to; past readyWait
	// (a slow transcode start) kino stops waiting, and the session carries
	// on. It is never killed: that would orphan a running player.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(readyWait):
		return nil
	case l := <-line:
		switch {
		case l == "ok":
			return nil
		case strings.HasPrefix(l, "error "):
			return errors.New(strings.TrimPrefix(l, "error "))
		default:
			return fmt.Errorf("the play session exited before the player started")
		}
	}
}

// RunSession is the play-session subcommand: it reads a Stream from in,
// launches the player, writes "ok" or "error <message>" to out once the
// play is playing or has failed, and follows it until the player exits or
// ctx ends.
func (s *Player) RunSession(ctx context.Context, in io.Reader, out io.Writer) error {
	tracker, ok := s.starter.(Tracker)
	if !ok {
		fmt.Fprintln(out, "error this server keeps no plays")
		return errors.New("server keeps no plays")
	}
	var stream media.Stream
	if err := json.NewDecoder(in).Decode(&stream); err != nil || stream.PlayID == "" || len(stream.Parts) == 0 {
		fmt.Fprintln(out, "error no play to follow")
		return fmt.Errorf("reading the play: %v", err)
	}
	ready := func(err error) {
		if err != nil {
			fmt.Fprintln(out, "error "+strings.ReplaceAll(err.Error(), "\n", " "))
			return
		}
		fmt.Fprintln(out, "ok")
	}
	return newSession(tracker, s.launcher, s.logger).run(ctx, stream, ready)
}
