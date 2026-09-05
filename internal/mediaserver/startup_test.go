package mediaserver

import (
	"github.com/mmcdole/kino/internal/config"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientConstructionDoesNotRequireNetwork(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	cfg := &config.Config{}
	cfg.Server.Type = config.SourceTypePlex
	cfg.Server.URL = srv.URL
	cfg.Server.Token = "token"
	if _, err := NewClient(cfg, nil); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("client construction blocked startup on network I/O")
	}
}
