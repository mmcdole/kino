package kivo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// playerCapabilities is what kino declares for the desktop players it
// launches, which play nearly anything. Dolby Vision is left out: how a
// DV-only stream looks depends on the player's video output, so the server
// decides. A file the player still can't open falls back to a transcode.
var playerCapabilities = capabilities{
	Containers: []string{"mkv", "mp4", "webm", "avi", "ts", "mov"},
	HLS:        true,
	Video: []videoCapability{
		{Codec: "h264", MaxBitDepth: 10},
		{Codec: "hevc", MaxBitDepth: 10, DynamicRanges: []string{"hdr10", "hlg"}},
		{Codec: "av1", MaxBitDepth: 10, DynamicRanges: []string{"hdr10", "hlg"}},
		{Codec: "vp9", MaxBitDepth: 10},
	},
	Audio: []audioCapability{
		{Codec: "aac", MaxChannels: 8},
		{Codec: "ac3", MaxChannels: 8},
		{Codec: "eac3", MaxChannels: 8},
		{Codec: "opus", MaxChannels: 8},
		{Codec: "flac", MaxChannels: 8},
		{Codec: "mp3", MaxChannels: 2},
		{Codec: "truehd", MaxChannels: 8},
		{Codec: "dts", MaxChannels: 8},
	},
	Subtitles: []string{"vtt", "ass", "pgs"},
}

// StartPlay starts a play of a work at a position on its timeline.
func (c *Client) StartPlay(ctx context.Context, itemID string, at time.Duration) (media.Stream, error) {
	req := playCreate{WorkID: itemID, PositionMs: at.Milliseconds(), Capabilities: playerCapabilities}
	var resp playStarted
	if err := c.send(ctx, http.MethodPost, "/plays", req, &resp); err != nil {
		return media.Stream{}, err
	}
	return c.stream(resp), nil
}

// ReportPlay is the heartbeat. A play the server no longer has is
// media.ErrItemNotFound.
func (c *Client) ReportPlay(ctx context.Context, playID string, p media.Progress) error {
	req := playUpdate{PositionMs: p.Position.Milliseconds(), State: string(p.State), SeekToFirstFrameMs: p.SeekToFirstFrame.Milliseconds()}
	return c.send(ctx, http.MethodPatch, "/plays/"+url.PathEscape(playID), req, nil)
}

// StopPlay saves the position and ends the play.
func (c *Client) StopPlay(ctx context.Context, playID string, at time.Duration) error {
	return c.send(ctx, http.MethodPost, "/plays/"+url.PathEscape(playID)+"/stop", playStop{PositionMs: at.Milliseconds()}, nil)
}

// FallbackPlay ends a play the player couldn't open and starts one at the
// same position that transcodes everything.
func (c *Client) FallbackPlay(ctx context.Context, playID string, at time.Duration, detail string) (media.Stream, error) {
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	req := fallbackRequest{PositionMs: at.Milliseconds(), Code: "mediaSourceError", Detail: detail}
	var resp playStarted
	if err := c.send(ctx, http.MethodPost, "/plays/"+url.PathEscape(playID)+"/fallback", req, &resp); err != nil {
		return media.Stream{}, err
	}
	return c.stream(resp), nil
}

// stream resolves a play's media URLs, which are relative to /api/v1/.
func (c *Client) stream(p playStarted) media.Stream {
	abs := func(u string) string { return c.baseURL + apiPrefix + "/" + strings.TrimPrefix(u, "/") }
	s := media.Stream{PlayID: p.ID, Start: millis(p.StartMs)}
	if p.Delivery.Method == "file" && len(p.Parts) > 0 {
		for _, part := range p.Parts {
			s.Parts = append(s.Parts, media.StreamPart{URL: abs(part.URL), Duration: millis(part.DurationMs)})
		}
		return s
	}
	s.Parts = []media.StreamPart{{URL: abs(p.Delivery.URL), Duration: millis(p.DurationMs)}}
	return s
}

// problemError turns the problem documents a user can act on into errors
// worded for them; anything else is returned as it is.
func problemError(err error) error {
	p, ok := decodeProblem(err)
	if !ok {
		return err
	}
	switch p.Code {
	case "streamLimit":
		var where []string
		for _, pl := range p.Plays {
			if pl.Client != "" {
				where = append(where, fmt.Sprintf("%s (%s)", pl.Client, pl.Title))
			} else {
				where = append(where, pl.Title)
			}
		}
		if len(where) == 0 {
			return &media.RefusedError{Message: "Your stream limit is reached. Stop a play elsewhere to play here."}
		}
		return &media.RefusedError{Message: "Already playing on " + strings.Join(where, " and ") + ". Stop one to play here."}
	case "transcodeBusy":
		if p.Reason == "storageFull" {
			return &media.RefusedError{Message: "The server's transcode disk is full."}
		}
		return &media.RefusedError{Message: "The server is busy transcoding. Try again in a minute."}
	case "notPlayable":
		return &media.RefusedError{Message: "Can't play this here: " + reasonText(p.Reason) + "."}
	case "noFallback":
		return media.ErrNoFallback
	}
	return err
}

// reasonText words a PlaybackReason for a notPlayable refusal.
func reasonText(reason string) string {
	switch reason {
	case "sourceUnreadable":
		return "the server can't read the file"
	case "transcodeFailed":
		return "the server failed to transcode it"
	case "keyframeIndexMissing":
		return "the server hasn't indexed the file yet"
	case "noTracks":
		return "it has nothing to play"
	case "videoCodecUnsupported", "audioCodecUnsupported", "containerUnsupported":
		return "its format can't be played or converted"
	case "":
		return "the server gave no reason"
	default:
		return reason
	}
}
