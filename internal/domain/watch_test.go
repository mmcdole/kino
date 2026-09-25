package domain

import (
	"testing"
	"time"
)

func TestWatchChangeApply(t *testing.T) {
	episode := func(played bool, offset time.Duration) *MediaItem {
		return &MediaItem{ID: "e", Type: MediaTypeEpisode, ShowID: "show", ParentID: "season", IsPlayed: played, ViewOffset: offset}
	}
	lists := func(e *MediaItem, unwatched int) map[string][]ListItem {
		return map[string][]ListItem{
			"episodes": {e},
			"playlist": {&MediaItem{ID: "other"}, e},
			"shows":    {&Show{ID: "show", EpisodeCount: 10, UnwatchedCount: unwatched}},
			"seasons":  {&Season{ID: "season", EpisodeCount: 10, UnwatchedCount: unwatched}},
			"movies":   {&MediaItem{ID: "m"}},
		}
	}
	tests := []struct {
		name      string
		input     map[string][]ListItem
		played    bool
		changed   []string
		unwatched int
	}{
		{"watch rolls up once across projections", lists(episode(false, time.Minute), 5), true, []string{"episodes", "playlist", "shows", "seasons"}, 4},
		{"unwatch restores the count", lists(episode(true, 0), 4), false, []string{"episodes", "playlist", "shows", "seasons"}, 5},
		{"already watched is a no-op", lists(episode(true, 0), 4), true, nil, 4},
		{"clearing resume position does not roll up", lists(episode(false, time.Minute), 5), false, []string{"episodes", "playlist"}, 5},
		{"count never drops below zero", lists(episode(false, 0), 0), true, []string{"episodes", "playlist", "shows", "seasons"}, 0},
		{"count never exceeds episode count", lists(episode(true, 0), 10), false, []string{"episodes", "playlist", "shows", "seasons"}, 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			change := WatchChange{ItemID: "e", ShowID: "show", SeasonID: "season", Played: test.played}
			got := change.Apply(test.input)
			if len(got) != len(test.changed) {
				t.Fatalf("changed %d lists, want %v", len(got), test.changed)
			}
			for _, key := range test.changed {
				if _, ok := got[key]; !ok {
					t.Fatalf("list %q not changed", key)
				}
			}
			if shows, ok := got["shows"]; ok && shows[0].(*Show).UnwatchedCount != test.unwatched {
				t.Fatalf("show unwatched = %d, want %d", shows[0].(*Show).UnwatchedCount, test.unwatched)
			}
			if items, ok := got["playlist"]; ok {
				if e := items[1].(*MediaItem); e.IsPlayed != test.played || e.ViewOffset != 0 {
					t.Fatalf("playlist copy not patched: %+v", e)
				}
			}
		})
	}
}

func TestWatchChangeLeavesInputUntouched(t *testing.T) {
	e := &MediaItem{ID: "e", ShowID: "show"}
	show := &Show{ID: "show", EpisodeCount: 2, UnwatchedCount: 2}
	input := map[string][]ListItem{"a": {e}, "b": {show}}
	WatchChange{ItemID: "e", ShowID: "show", Played: true}.Apply(input)
	if e.IsPlayed || show.UnwatchedCount != 2 || input["a"][0] != e || input["b"][0] != show {
		t.Fatal("Apply modified its input")
	}
}
