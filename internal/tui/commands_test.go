package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mmcdole/kino/internal/catalog"
)

type streamingCatalog struct {
	Catalog
	load func(context.Context, catalog.Observer) (catalog.Snapshot, error)
}

func (s streamingCatalog) Load(ctx context.Context, _ catalog.Resource, _ catalog.Policy, o catalog.Observer) (catalog.Snapshot, error) {
	return s.load(ctx, o)
}

func TestTerminalResultSurvivesProgressFlood(t *testing.T) {
	req := newRequests(context.Background()).begin("test", catalog.Resource{}, catalog.Browse)
	defer req.cancel()
	svc := streamingCatalog{load: func(ctx context.Context, o catalog.Observer) (catalog.Snapshot, error) {
		for i := range 10000 {
			o.Progress(catalog.Progress{Loaded: i, Total: 10000})
		}
		return catalog.Snapshot{Revision: 42}, errors.New("terminal error")
	}}
	msg := LoadResourceCmd(svc, req)().(ResourceMsg)
	for msg.Stage != loadFinished {
		if msg.Next == nil {
			t.Fatal("progress chain lost its continuation")
		}
		msg = msg.Next().(ResourceMsg)
	}
	if msg.Snapshot.Revision != 42 || msg.Err == nil {
		t.Fatal("terminal result was dropped")
	}
}

func TestCanceledCommandDoesNotStrandProducer(t *testing.T) {
	req := newRequests(context.Background()).begin("test", catalog.Resource{}, catalog.Browse)
	completed := make(chan struct{})
	svc := streamingCatalog{load: func(ctx context.Context, o catalog.Observer) (catalog.Snapshot, error) {
		defer close(completed)
		<-ctx.Done()
		return catalog.Snapshot{}, ctx.Err()
	}}
	req.cancel()
	msg := LoadResourceCmd(svc, req)().(ResourceMsg)
	if msg.Stage != loadFinished || !errors.Is(msg.Err, context.Canceled) {
		t.Fatal("cancellation did not terminate command")
	}
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("canceled producer stranded")
	}
}
