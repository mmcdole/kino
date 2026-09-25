package store

import (
	"encoding/json"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// listItemWrapper wraps ListItem for JSON serialization
type listItemWrapper struct {
	Type     string          `json:"type"`
	Movie    *media.Item     `json:"movie,omitempty"`
	Show     *media.Show     `json:"show,omitempty"`
	Season   *media.Season   `json:"season,omitempty"`
	Library  *media.Library  `json:"library,omitempty"`
	Playlist *media.Playlist `json:"playlist,omitempty"`
}

type storedSnapshot struct {
	Items     []listItemWrapper `json:"items"`
	FetchedAt time.Time         `json:"fetched_at"`
	Version   int64             `json:"version"`
}

func decodeSnapshot(data []byte) (media.CachedList, bool) {
	var snapshot storedSnapshot
	if data == nil || json.Unmarshal(data, &snapshot) != nil {
		return media.CachedList{}, false
	}
	return media.CachedList{Items: unwrapListItems(snapshot.Items), FetchedAt: snapshot.FetchedAt, Version: snapshot.Version}, true
}

func encodeSnapshot(l media.CachedList) ([]byte, error) {
	return json.Marshal(storedSnapshot{Items: wrapListItems(l.Items), FetchedAt: l.FetchedAt, Version: l.Version})
}

// wrapListItems converts media.ListItem slice to serializable wrappers
func wrapListItems(items []media.ListItem) []listItemWrapper {
	wrappers := make([]listItemWrapper, len(items))
	for i, item := range items {
		switch v := item.(type) {
		case *media.Item:
			wrappers[i] = listItemWrapper{Type: "movie", Movie: v}
		case *media.Show:
			wrappers[i] = listItemWrapper{Type: "show", Show: v}
		case *media.Season:
			wrappers[i] = listItemWrapper{Type: "season", Season: v}
		case *media.Library:
			wrappers[i] = listItemWrapper{Type: "library", Library: v}
		case *media.Playlist:
			wrappers[i] = listItemWrapper{Type: "playlist", Playlist: v}
		}
	}
	return wrappers
}

// unwrapListItems converts wrappers back to media.ListItem slice
func unwrapListItems(wrappers []listItemWrapper) []media.ListItem {
	items := make([]media.ListItem, 0, len(wrappers))
	for _, w := range wrappers {
		switch w.Type {
		case "movie":
			if w.Movie != nil {
				items = append(items, w.Movie)
			}
		case "show":
			if w.Show != nil {
				items = append(items, w.Show)
			}
		case "season":
			if w.Season != nil {
				items = append(items, w.Season)
			}
		case "library":
			if w.Library != nil {
				items = append(items, w.Library)
			}
		case "playlist":
			if w.Playlist != nil {
				items = append(items, w.Playlist)
			}
		}
	}
	return items
}
