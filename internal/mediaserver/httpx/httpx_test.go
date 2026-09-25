package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func TestStatusMapping(t *testing.T) {
	tests := []struct {
		status   int
		retry    bool
		want     error
		attempts int32
	}{
		{http.StatusOK, true, nil, 1},
		{http.StatusNoContent, false, nil, 1},
		{http.StatusUnauthorized, true, domain.ErrAuthFailed, 1},
		{http.StatusNotFound, true, domain.ErrItemNotFound, 1},
		{http.StatusBadRequest, true, nil, 1},
		{http.StatusBadGateway, true, nil, 3},
		{http.StatusBadGateway, false, nil, 1},
	}
	for _, test := range tests {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.WriteHeader(test.status)
		}))
		c := &Client{BaseURL: server.URL, HTTP: server.Client(), Retries: 2, Logger: slog.New(slog.DiscardHandler)}
		_, err := c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/", Retry: test.retry})
		server.Close()

		if got := attempts.Load(); got != test.attempts {
			t.Errorf("status %d retry=%v: %d attempts, want %d", test.status, test.retry, got, test.attempts)
		}
		ok := test.status >= 200 && test.status < 300
		if ok != (err == nil) || (test.want != nil && !errors.Is(err, test.want)) {
			t.Errorf("status %d: err = %v, want %v", test.status, err, test.want)
		}
	}
}

func TestBodyAndHeadersAreSentOnEveryAttempt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 16)
		n, _ := r.Body.Read(body)
		if string(body[:n]) != `{"a":1}` || r.Header.Get("X-Token") != "t" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("attempt %d: body %q headers %v", attempts.Load(), body[:n], r.Header)
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	c := &Client{
		BaseURL: server.URL, HTTP: server.Client(), Retries: 1, Logger: slog.New(slog.DiscardHandler),
		Header: func(h http.Header) { h.Set("X-Token", "t") },
	}
	if _, err := c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/", Body: []byte(`{"a":1}`), Retry: true}); err != nil {
		t.Fatal(err)
	}
}
