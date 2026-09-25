package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// Membership lists every playlist and whether each one holds an item.
type Membership struct {
	Playlists []*domain.Playlist
	Present   map[string]bool
}

// membershipFetches bounds how many playlists are checked at once.
const membershipFetches = 4

// PlaylistMembership fetches the same playlist snapshot that the modal will
// display. Every membership is verified; unknown never means absent.
func (s *Service) PlaylistMembership(ctx context.Context, itemID string) (Membership, error) {
	ctx, finish, startErr := s.operation(ctx, 30*time.Second)
	if startErr != nil {
		return Membership{}, startErr
	}
	defer finish()

	snapshot, err := s.Load(ctx, Resource{Kind: Playlists}, Revalidate)
	if err != nil {
		return Membership{}, err
	}
	result := Membership{Present: make(map[string]bool)}
	for _, item := range snapshot.Items {
		if p, ok := item.(*domain.Playlist); ok {
			result.Playlists = append(result.Playlists, p)
		}
	}

	var (
		mu       sync.Mutex
		failures []error
		wg       sync.WaitGroup
		slots    = make(chan struct{}, membershipFetches)
	)
	for _, playlist := range result.Playlists {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Go(func() {
			defer func() { <-slots }()
			items, err := s.Load(ctx, Resource{Kind: PlaylistItems, ID: playlist.ID}, Revalidate)
			present := slices.ContainsFunc(items.Items, func(item domain.ListItem) bool { return item.GetID() == itemID })
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, fmt.Errorf("playlist %q: %w", playlist.Title, err))
				return
			}
			result.Present[playlist.ID] = present
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return Membership{}, errors.Join(failures...)
	}
	return result, nil
}
