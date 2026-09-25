package media

import "time"

// CachedList is a detached snapshot. FetchedAt records a successful fetch,
// never a read or local watch-state edit. Version is the server's revision,
// when available; its meaning is independent of the local clock.
type CachedList struct {
	Items     []ListItem
	FetchedAt time.Time
	Version   int64
}
