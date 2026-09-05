package domain

import "time"

// CachedList is a detached snapshot. FetchedAt records a successful fetch,
// never a read or local watch-state edit. Version is the server's revision,
// when available; its meaning is independent of the local clock.
type CachedList struct {
	Items     []ListItem
	FetchedAt time.Time
	Version   int64
}

// CloneItems copies entity values so consumers can retain or modify snapshots
// independently of other requests and projections.
func CloneItems(items []ListItem) []ListItem {
	out := make([]ListItem, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case *MediaItem:
			c := *v
			out = append(out, &c)
		case *Show:
			c := *v
			out = append(out, &c)
		case *Season:
			c := *v
			out = append(out, &c)
		case *Library:
			c := *v
			out = append(out, &c)
		case *Playlist:
			c := *v
			out = append(out, &c)
		}
	}
	return out
}

// ListItems widens a slice of entities without copying their values.
func ListItems[T ListItem](items []T) []ListItem {
	out := make([]ListItem, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}
