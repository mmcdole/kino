package domain

import "context"

// SearchClient provides network search functionality.
type SearchClient interface {
	Search(ctx context.Context, query string) ([]*MediaItem, error)
}
