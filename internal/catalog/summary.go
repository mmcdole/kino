package catalog

import (
	"context"
	"errors"
	"sync"
)

// SummaryBackend is a Backend whose listings leave out synopses, so the
// inspector asks for one item's at a time.
type SummaryBackend interface {
	GetSummary(ctx context.Context, itemID string) (string, error)
}

// ErrNoSummaries is a summary request on a server whose listings already
// carry them.
var ErrNoSummaries = errors.New("this server lists summaries with items")

// summaries remembers fetched synopses for the session. An item with none is
// remembered as empty, so it isn't asked for again.
type summaries struct {
	mu   sync.Mutex
	text map[string]string
}

// FetchesSummaries reports whether the inspector must ask for synopses.
func (s *Service) FetchesSummaries() bool { return s.summaryBackend != nil }

// Summary returns an item's synopsis, empty when it has none.
func (s *Service) Summary(ctx context.Context, itemID string) (string, error) {
	if s.summaryBackend == nil {
		return "", ErrNoSummaries
	}
	s.summaries.mu.Lock()
	text, ok := s.summaries.text[itemID]
	s.summaries.mu.Unlock()
	if ok {
		return text, nil
	}
	ctx, finish, err := s.operation(ctx, requestTimeout)
	if err != nil {
		return "", err
	}
	defer finish()
	text, err = s.summaryBackend.GetSummary(ctx, itemID)
	if err != nil {
		return "", err
	}
	s.summaries.mu.Lock()
	if s.summaries.text == nil {
		s.summaries.text = make(map[string]string)
	}
	s.summaries.text[itemID] = text
	s.summaries.mu.Unlock()
	return text, nil
}
