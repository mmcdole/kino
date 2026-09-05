package mediaserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

// Exercise the public backend contract, including non-retried mutations:
// cancellation during a request must not be reported as an offline server.
func TestBackendCancellationAndNotFound(t *testing.T) {
	for _, backend := range []string{"plex", "jellyfin"} {
		t.Run(backend, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("key") == "missing" || r.URL.Path == "/Users/user/PlayedItems/missing" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			var client domain.PlaybackClient
			if backend == "plex" {
				client = plex.NewClient(server.URL, "token", "device", nil)
			} else {
				client = jellyfin.NewClient(server.URL, "token", "user", "device", nil)
			}
			if err := client.MarkPlayed(context.Background(), "missing"); !errors.Is(err, domain.ErrItemNotFound) {
				t.Fatalf("404: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- client.MarkPlayed(ctx, "item") }()
			<-started
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrServerOffline) {
				t.Fatalf("canceled request: %v", err)
			}
		})
	}
}
