package plex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "tok", "client1", nil)
	c.machineIdentifier = "machine1"
	return c
}

// Every request path — including playlist mutations — must map 401 to
// ErrAuthFailed so the TUI's re-auth prompt fires.
func TestMutations401MapToErrAuthFailed(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	ctx := context.Background()

	calls := map[string]error{
		"MarkPlayed":     c.MarkPlayed(ctx, "1"),
		"AddToPlaylist":  c.AddToPlaylist(ctx, "p", []string{"1"}),
		"DeletePlaylist": c.DeletePlaylist(ctx, "p"),
	}
	if _, err := c.CreatePlaylist(ctx, "t", []string{"1"}); err != nil {
		calls["CreatePlaylist"] = err
	}

	for name, err := range calls {
		if !errors.Is(err, domain.ErrAuthFailed) {
			t.Errorf("%s: 401 not mapped to ErrAuthFailed, got %v", name, err)
		}
	}
}

// Network errors wrap ErrServerOffline while preserving the cause.
func TestNetworkErrorWrapsServerOffline(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "tok", "client1", nil)
	err := c.DeletePlaylist(context.Background(), "p")
	if !errors.Is(err, domain.ErrServerOffline) {
		t.Fatalf("network error not mapped: %v", err)
	}
	if err.Error() == domain.ErrServerOffline.Error() {
		t.Fatalf("underlying cause discarded: %v", err)
	}
}

// GETs retry on 5xx (previously Plex had no retry at all); mutations don't.
func TestRetryPolicy(t *testing.T) {
	var gets, deletes atomic.Int32
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
		} else {
			deletes.Add(1)
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

	if err := c.DeletePlaylist(ctx, "p"); err == nil {
		t.Fatal("expected error")
	}
	if got := deletes.Load(); got != 1 {
		t.Fatalf("mutation attempts = %d, want 1 (no retry)", got)
	}
}

// Scrobble endpoints must carry the identifier parameter some PMS versions
// require.
func TestScrobbleIdentifierParam(t *testing.T) {
	var query url.Values
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write([]byte(`{}`))
	}))
	if err := c.MarkPlayed(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
	if got := query.Get("identifier"); got != "com.plexapp.plugins.library" {
		t.Fatalf("scrobble identifier = %q", got)
	}
	if got := query.Get("key"); got != "42" {
		t.Fatalf("scrobble key = %q", got)
	}
}

// Global search results include TV shows (parity with the Jellyfin backend).
func TestSearchIncludesShows(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"MediaContainer":{"Metadata":[
			{"ratingKey":"1","title":"A Movie","type":"movie"},
			{"ratingKey":"2","title":"A Show","type":"show","year":2020}
		]}}`))
	}))

	results, err := c.Search(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (show dropped?)", len(results))
	}
	var foundShow bool
	for _, r := range results {
		if r.Type == domain.MediaTypeShow && r.Title == "A Show" {
			foundShow = true
		}
	}
	if !foundShow {
		t.Fatal("show missing from search results")
	}
}

func TestPlaylistIdentityIsLazySharedAndRetryable(t *testing.T) {
	for _, payload := range []string{
		`<MediaContainer machineIdentifier="server1"/>`,
		`{"MediaContainer":{"machineIdentifier":"server1"}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			var identities, writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/identity" {
					if identities.Add(1) == 1 {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					fmt.Fprint(w, payload)
					return
				}
				if !strings.HasPrefix(r.URL.Query().Get("uri"), "server://server1/") {
					t.Error("mutation used missing or incorrect server identity")
				}
				writes.Add(1)
			}))
			defer srv.Close()
			c := NewClient(srv.URL, "token", "client", nil)
			if err := c.AddToPlaylist(context.Background(), "p", []string{"m"}); !errors.Is(err, domain.ErrAuthFailed) {
				t.Fatalf("identity error lost classification: %v", err)
			}
			if writes.Load() != 0 {
				t.Fatal("write attempted without identity")
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if err := c.AddToPlaylist(context.Background(), "p", []string{"m"}); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			if identities.Load() != 2 || writes.Load() != 8 {
				t.Fatal("identity not retried then shared")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := c.AddToPlaylist(ctx, "p", []string{"m"}); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if writes.Load() != 8 {
				t.Fatal("canceled request wrote to server")
			}
		})
	}
}
