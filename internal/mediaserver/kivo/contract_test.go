package kivo

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// The spec lives in the Kivo repository, which is private: the contract
// checks run where it is checked out beside kino (or at $KIVO_OPENAPI) and
// are skipped elsewhere. The fixtures in testdata are responses recorded from
// a real server.
func specPath() string {
	if p := os.Getenv("KIVO_OPENAPI"); p != "" {
		return p
	}
	return filepath.Join("..", "..", "..", "..", "kivo", "server", "internal", "api", "openapi.yaml")
}

var (
	specOnce   sync.Once
	specRouter routers.Router
	specErr    error
)

func loadSpec() (routers.Router, error) {
	specOnce.Do(func() {
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromFile(specPath())
		if err != nil {
			specErr = err
			return
		}
		if err := doc.Validate(context.Background()); err != nil {
			specErr = err
			return
		}
		specRouter, specErr = gorillamux.NewRouter(doc)
	})
	return specRouter, specErr
}

// route is one canned response of a fake Kivo.
type route struct {
	status  int
	fixture string // file in testdata; empty for no body
}

// fakeKivo serves canned responses by "METHOD /path" (path under /api/v1,
// without the query). When the spec is available, it checks every request
// it gets and every response it sends against it, failing the test on a
// mismatch.
type fakeKivo struct {
	t      *testing.T
	routes map[string]route
	mu     sync.Mutex
	got    []recorded
}

type recorded struct {
	method, path, query string
	body                []byte
}

func newFakeKivo(t *testing.T, routes map[string]route) (*fakeKivo, *httptest.Server) {
	t.Helper()
	f := &fakeKivo{t: t, routes: routes}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeKivo) requests() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recorded(nil), f.got...)
}

func (f *fakeKivo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := strings.TrimPrefix(r.URL.Path, apiPrefix)
	f.mu.Lock()
	f.got = append(f.got, recorded{method: r.Method, path: path, query: r.URL.RawQuery, body: body})
	f.mu.Unlock()

	rt, ok := f.routes[r.Method+" "+path]
	if !ok {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
		return
	}
	var out []byte
	if rt.fixture != "" {
		var err error
		if out, err = os.ReadFile(filepath.Join("testdata", rt.fixture)); err != nil {
			f.t.Fatal(err)
		}
	}
	f.validate(r, body, rt.status, out)
	contentType := "application/json"
	if rt.status >= 400 {
		contentType = "application/problem+json"
	}
	if out != nil {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(rt.status)
	_, _ = w.Write(out)
}

func (f *fakeKivo) validate(r *http.Request, body []byte, status int, out []byte) {
	router, err := loadSpec()
	if err != nil {
		return // no spec here; the fixtures still exercise the client
	}
	// Match the spec's server ("/"): the request as the server sees it.
	req := r.Clone(context.Background())
	req.Body = io.NopCloser(bytes.NewReader(body))
	route, params, err := router.FindRoute(req)
	if err != nil {
		f.t.Errorf("%s %s is not in the spec: %v", r.Method, r.URL.Path, err)
		return
	}
	in := &openapi3filter.RequestValidationInput{
		Request: req, PathParams: params, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
		f.t.Errorf("request %s %s breaks the spec: %v", r.Method, r.URL.Path, err)
	}
	contentType := "application/json"
	if status >= 400 {
		contentType = "application/problem+json"
	}
	header := http.Header{}
	if out != nil {
		header.Set("Content-Type", contentType)
	}
	resp := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in, Status: status, Header: header,
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	resp.SetBodyBytes(out)
	if err := openapi3filter.ValidateResponse(context.Background(), resp); err != nil {
		f.t.Errorf("fixture %d for %s %s breaks the spec: %v", status, r.Method, r.URL.Path, err)
	}
}

// The spec loads where it's checked out; a broken path would quietly turn
// every contract check off.
func TestSpecLoads(t *testing.T) {
	if _, err := os.Stat(specPath()); err != nil {
		t.Skipf("Kivo spec not checked out at %s", specPath())
	}
	if _, err := loadSpec(); err != nil {
		t.Fatalf("spec: %v", err)
	}
}
