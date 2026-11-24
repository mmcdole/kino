package menu

import (
	"testing"

	"github.com/mmcdole/kino/internal/media"
)

func labels(entries []Entry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Label)
	}
	return out
}

func TestEntriesLabelsAreUniqueAndSorted(t *testing.T) {
	movies := media.Library{ID: "m", Name: "Movies", Type: "movie"}
	kids := media.Library{ID: "k", Name: "Kids", Type: "movie"}
	tv := media.Library{ID: "t", Name: "TV", Type: "show"}
	entries := Entries([]Library{
		{Library: movies, Items: []media.ListItem{
			&media.Item{ID: "1", Title: "Heat", Year: 1995, Type: media.TypeMovie},
			&media.Item{ID: "2", Title: "Dune", Year: 2021, Type: media.TypeMovie},
			&media.Item{ID: "3", Title: "Dune", Year: 2021, Type: media.TypeMovie},
			&media.Item{ID: "4", Title: "Line\nBreak", Type: media.TypeMovie},
		}},
		{Library: kids, Items: []media.ListItem{
			&media.Item{ID: "5", Title: "Heat", Year: 1995, Type: media.TypeMovie},
		}},
		{Library: tv, Items: []media.ListItem{
			&media.Show{ID: "6", Title: "Severance", Year: 2022},
			&media.Item{ID: "7", Title: "An Episode", Type: media.TypeEpisode},
		}},
	})
	want := []string{
		"Dune (2021) - Movies #1",
		"Dune (2021) - Movies #2",
		"Heat (1995) - Kids",
		"Heat (1995) - Movies",
		"Line Break",
		"Severance (2022) [TV]",
	}
	got := labels(entries)
	if len(got) != len(want) {
		t.Fatalf("labels = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %q, want %q", got, want)
		}
	}
	e, ok := Find(entries, "Heat (1995) - Kids\n")
	if !ok || e.Item.GetID() != "5" {
		t.Fatalf("Find = %v, %v", e, ok)
	}
	if _, ok := Find(entries, "Heat"); ok {
		t.Fatal("Find matched a partial line")
	}
}

func ep(season, num int, played bool) *media.Item {
	return &media.Item{ID: string(rune('a'+season)) + string(rune('0'+num)), SeasonNum: season, EpisodeNum: num, IsPlayed: played, Type: media.TypeEpisode}
}

func TestNextEpisode(t *testing.T) {
	specials := Season{Episodes: []*media.Item{ep(0, 1, false)}}
	s1 := Season{Episodes: []*media.Item{ep(1, 2, false), ep(1, 1, true)}}
	s2 := Season{Episodes: []*media.Item{ep(2, 1, false)}}

	got, ok := NextEpisode([]Season{specials, s2, s1})
	if !ok || got.SeasonNum != 1 || got.EpisodeNum != 2 {
		t.Fatalf("next = %+v, want S1E2", got)
	}

	done := Season{Episodes: []*media.Item{ep(1, 2, true), ep(1, 1, true)}}
	got, ok = NextEpisode([]Season{done})
	if !ok || got.EpisodeNum != 1 {
		t.Fatalf("watched show: next = %+v, want S1E1", got)
	}

	if _, ok := NextEpisode(nil); ok {
		t.Fatal("empty show has a next episode")
	}
}

func TestEpisodesPutsUpNextFirst(t *testing.T) {
	watched := ep(1, 1, true)
	watched.Title = "Pilot"
	partial := ep(1, 2, false)
	partial.Title = "Second"
	partial.ViewOffset = 5
	entries := Episodes([]Season{{Episodes: []*media.Item{partial, watched}}})
	want := []string{"▶ Up next: S01E02  Second", "S01E01  Pilot  ✓", "S01E02  Second  ◐"}
	got := labels(entries)
	if len(got) != len(want) {
		t.Fatalf("labels = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("labels = %q, want %q", got, want)
		}
	}
	if e, ok := Find(entries, want[0]); !ok || e.Item != partial {
		t.Fatalf("Find(up next) = %v, %v", e, ok)
	}
}
