package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"
)

// mpvEvent is one line from mpv's JSON IPC: an event, or a reply to a
// command (which has no Event).
type mpvEvent struct {
	Event     string          `json:"event"`
	Name      string          `json:"name"`       // property-change: the property
	Data      json.RawMessage `json:"data"`       // property-change: its value, null when unavailable
	Reason    string          `json:"reason"`     // end-file: eof, stop, quit, error or redirect
	FileError string          `json:"file_error"` // end-file with error: mpv's words for it
}

// mpvConn is a connection to a running mpv's IPC socket. Events closes when
// mpv goes away.
type mpvConn struct {
	conn   net.Conn
	Events <-chan mpvEvent
}

// mpvProperties are what a play session follows.
var mpvProperties = []string{"time-pos", "pause", "playlist-pos"}

// dialMPV connects to the socket mpv was told to listen on, retrying while mpv
// starts, and observes mpvProperties.
func dialMPV(ctx context.Context, path string) (*mpvConn, error) {
	var d net.Dialer
	var conn net.Conn
	for {
		var err error
		conn, err = d.DialContext(ctx, "unix", path)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	events := make(chan mpvEvent, 64)
	c := &mpvConn{conn: conn, Events: events}
	for i, name := range mpvProperties {
		if err := c.command("observe_property", i+1, name); err != nil {
			conn.Close()
			return nil, err
		}
	}
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			var e mpvEvent
			if json.Unmarshal(scanner.Bytes(), &e) != nil || e.Event == "" {
				continue
			}
			events <- e
		}
	}()
	return c, nil
}

func (c *mpvConn) command(args ...any) error {
	line, err := json.Marshal(map[string]any{"command": args})
	if err != nil {
		return err
	}
	_, err = c.conn.Write(append(line, '\n'))
	return err
}

func (c *mpvConn) Close() error { return c.conn.Close() }

// seconds decodes a property's numeric value; ok is false for null.
func (e mpvEvent) seconds() (float64, bool) {
	var v *float64
	if json.Unmarshal(e.Data, &v) != nil || v == nil {
		return 0, false
	}
	return *v, true
}

// flag decodes a property's boolean value.
func (e mpvEvent) flag() bool {
	var v bool
	_ = json.Unmarshal(e.Data, &v)
	return v
}
