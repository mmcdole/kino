package jellyfin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mmcdole/kino/internal/media"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "tok", "user1", "dev1", nil)
	c.api.RetryDelay = 0
	return c
}

// Every request path — including mutations — must map 401 to ErrAuthFailed
// so the TUI's re-auth prompt fires.
func TestMutations401MapToErrAuthFailed(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	ctx := context.Background()

	calls := map[string]error{
		"MarkPlayed":     c.MarkPlayed(ctx, "x"),
		"MarkUnplayed":   c.MarkUnplayed(ctx, "x"),
		"AddToPlaylist":  c.AddToPlaylist(ctx, "p", []string{"x"}),
		"DeletePlaylist": c.DeletePlaylist(ctx, "p"),
	}
	if _, err := c.CreatePlaylist(ctx, "t", []string{"x"}); err != nil {
		calls["CreatePlaylist"] = err
	}
	if err := c.RemoveFromPlaylist(ctx, "p", "x"); err != nil {
		calls["RemoveFromPlaylist"] = err
	}

	for name, err := range calls {
		if !errors.Is(err, media.ErrAuthFailed) {
			t.Errorf("%s: 401 not mapped to ErrAuthFailed, got %v", name, err)
		}
	}
}

// Network errors wrap ErrServerOffline while preserving the cause.
func TestNetworkErrorWrapsServerOffline(t *testing.T) {
	c := NewClient(closedURL(t), "tok", "user1", "dev1", nil)
	err := c.MarkPlayed(context.Background(), "x")
	if !errors.Is(err, media.ErrServerOffline) {
		t.Fatalf("network error not mapped: %v", err)
	}
	if err.Error() == media.ErrServerOffline.Error() {
		t.Fatalf("underlying cause discarded: %v", err)
	}
}

// Idempotent requests retry on 5xx; mutations do not.
func TestRetryPolicy(t *testing.T) {
	var gets, posts atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		} else {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	ctx := context.Background()

	if _, err := c.GetLibraries(ctx); err == nil {
		t.Fatal("expected error")
	}
	if got := gets.Load(); got != int32(maxRetries+1) {
		t.Fatalf("GET attempts = %d, want %d", got, maxRetries+1)
	}

	if err := c.MarkPlayed(ctx, "x"); err == nil {
		t.Fatal("expected error")
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("mutation attempts = %d, want 1 (no retry)", got)
	}
}

// 204 No Content responses are success for mutations.
func Test2xxAccepted(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.MarkPlayed(context.Background(), "x"); err != nil {
		t.Fatalf("204 rejected: %v", err)
	}
}

// RemoveFromPlaylist resolves the playlist entry ID and sends it as
// EntryIds — sending the media item ID is silently ignored by Jellyfin.
func TestRemoveFromPlaylistUsesEntryID(t *testing.T) {
	var deleteQuery url.Values
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`{"Items":[{"Id":"item1","Name":"Ep","Type":"Episode","PlaylistItemId":"entry-guid-1"}],"TotalRecordCount":1}`))
		case http.MethodDelete:
			deleteQuery = r.URL.Query()
			w.WriteHeader(http.StatusNoContent)
		}
	}))

	if err := c.RemoveFromPlaylist(context.Background(), "pl1", "item1"); err != nil {
		t.Fatal(err)
	}
	if got := deleteQuery.Get("EntryIds"); got != "entry-guid-1" {
		t.Fatalf("EntryIds = %q, want entry-guid-1 (the playlist entry ID, not the item ID)", got)
	}
}

// The auth header carries the per-install device ID on every request.
func TestDeviceIDInAuthHeader(t *testing.T) {
	var header string
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("Authorization")
		w.Write([]byte(`{"Items":[],"TotalRecordCount":0}`))
	}))
	if _, err := c.GetLibraries(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`MediaBrowser `, `DeviceId="dev1"`, `Token="tok"`} {
		if !strings.Contains(header, want) {
			t.Fatalf("auth header missing %s: %q", want, header)
		}
	}
}

// Jellyfin 12 rejects the legacy X-Emby-Authorization header by default, so
// login must use the standard Authorization header.
func TestAuthenticateUsesAuthorizationHeader(t *testing.T) {
	var header, legacy string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("Authorization")
		legacy = r.Header.Get("X-Emby-Authorization")
		w.Write([]byte(`{"AccessToken":"tok","User":{"Id":"u1","Name":"jack"}}`))
	}))
	t.Cleanup(srv.Close)

	creds, err := Authenticate(context.Background(), srv.URL, "dev1", "jack", "pw", nil)
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token != "tok" {
		t.Fatalf("token = %q, want tok", creds.Token)
	}
	if !strings.HasPrefix(header, "MediaBrowser ") || !strings.Contains(header, `DeviceId="dev1"`) {
		t.Fatalf("Authorization header = %q", header)
	}
	if strings.Contains(header, "Token=") {
		t.Fatalf("login header carries a token: %q", header)
	}
	if legacy != "" {
		t.Fatalf("legacy X-Emby-Authorization header sent: %q", legacy)
	}
}

// Stream URLs carry both token spellings so old and new servers accept them.
func TestPlayableURLCarriesBothTokenSpellings(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"MediaSources":[{"Container":"mkv"}]}`))
	}))
	got, err := c.ResolvePlayableURL(context.Background(), "item1")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/Videos/item1/stream.mkv" {
		t.Fatalf("path = %q", u.Path)
	}
	q := u.Query()
	if q.Get("ApiKey") != "tok" || q.Get("api_key") != "tok" {
		t.Fatalf("query = %q, want ApiKey=tok and api_key=tok", u.RawQuery)
	}
}

// closedURL returns the address of a local port that just stopped listening,
// so connections are refused immediately instead of timing out.
func closedURL(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr
}
