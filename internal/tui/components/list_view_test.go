package components

import (
	"slices"
	"testing"

	"github.com/mmcdole/kino/internal/domain"
)

func ids(v *listView) []string {
	var out []string
	for pos := range v.len() {
		out = append(out, v.at(pos).GetID())
	}
	return out
}

func TestListViewSortAndFilter(t *testing.T) {
	items := []domain.ListItem{
		&domain.MediaItem{ID: "b", Title: "Bravo", Year: 2001, Rating: 7},
		&domain.MediaItem{ID: "a", Title: "Alpha", Year: 1999, Rating: 9},
		&domain.MediaItem{ID: "c", Title: "Charlie", Year: 2010, Rating: 5},
	}
	tests := []struct {
		name  string
		field SortField
		dir   SortDirection
		query string
		want  []string
	}{
		{"natural order", SortDefault, SortAsc, "", []string{"b", "a", "c"}},
		{"title ascending", SortTitle, SortAsc, "", []string{"a", "b", "c"}},
		{"title descending", SortTitle, SortDesc, "", []string{"c", "b", "a"}},
		{"release year", SortReleased, SortDesc, "", []string{"c", "b", "a"}},
		{"rating", SortRating, SortDesc, "", []string{"a", "b", "c"}},
		{"filter ranks the best match first", SortTitle, SortDesc, "alp", []string{"a"}},
		{"filter with no matches", SortTitle, SortAsc, "zzz", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := &listView{sortField: test.field, sortDir: test.dir, query: test.query}
			v.setItems(items)
			if got := ids(v); !slices.Equal(got, test.want) {
				t.Fatalf("order = %v, want %v", got, test.want)
			}
			if test.want == nil && v.selected() != nil {
				t.Fatal("an empty filter result has a selection")
			}
		})
	}
}

func TestListViewEpisodeOrder(t *testing.T) {
	v := &listView{sortField: SortEpisodeNum}
	v.setItems([]domain.ListItem{
		&domain.MediaItem{ID: "s2e1", SeasonNum: 2, EpisodeNum: 1},
		&domain.MediaItem{ID: "s1e2", SeasonNum: 1, EpisodeNum: 2},
		&domain.MediaItem{ID: "s1e1", SeasonNum: 1, EpisodeNum: 1},
	})
	if got := ids(v); !slices.Equal(got, []string{"s1e1", "s1e2", "s2e1"}) {
		t.Fatalf("order = %v", got)
	}
}

func TestListViewKeepsSelectionAcrossReplacement(t *testing.T) {
	v := &listView{sortField: SortTitle, rows: 2}
	v.setItems(testMovies("Alpha", "Bravo", "Charlie"))
	v.selectID("id-Charlie")
	if v.offset != 1 {
		t.Fatalf("cursor off screen: offset %d", v.offset)
	}
	v.setItems(testMovies("Anchor", "Alpha", "Bravo", "Charlie"))
	if v.selected().GetID() != "id-Charlie" {
		t.Fatal("selection moved when an item was added before it")
	}
	v.setItems(testMovies("Alpha"))
	if v.selected().GetID() != "id-Alpha" || v.offset != 0 {
		t.Fatal("cursor not clamped when the selected item disappeared")
	}
}
