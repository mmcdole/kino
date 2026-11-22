package catalog

import (
	"context"
	"errors"
	"testing"
)

type summaryServer struct {
	fakeBackend
	calls int
}

func (s *summaryServer) GetSummary(_ context.Context, id string) (string, error) {
	s.calls++
	if id == "bad" {
		return "", errors.New("offline")
	}
	if id == "none" {
		return "", nil
	}
	return "about " + id, nil
}

// A summary is fetched once per session, even when the item has none; a
// failure isn't remembered.
func TestSummaryRemembered(t *testing.T) {
	backend := &summaryServer{}
	svc, _ := testService(t, backend)
	if !svc.FetchesSummaries() {
		t.Fatal("summary backend not detected")
	}
	ctx := context.Background()
	for range 2 {
		if text, err := svc.Summary(ctx, "m"); err != nil || text != "about m" {
			t.Fatalf("summary = %q, %v", text, err)
		}
		if text, err := svc.Summary(ctx, "none"); err != nil || text != "" {
			t.Fatalf("summary = %q, %v", text, err)
		}
	}
	if backend.calls != 2 {
		t.Fatalf("calls = %d", backend.calls)
	}
	svc.Summary(ctx, "bad")
	svc.Summary(ctx, "bad")
	if backend.calls != 4 {
		t.Fatalf("failure was remembered: calls = %d", backend.calls)
	}
}

func TestSummaryWithoutBackend(t *testing.T) {
	svc, _ := testService(t, fakeBackend{})
	if svc.FetchesSummaries() {
		t.Fatal("listing backend asks for summaries")
	}
	if _, err := svc.Summary(context.Background(), "m"); !errors.Is(err, ErrNoSummaries) {
		t.Fatalf("err = %v", err)
	}
}
