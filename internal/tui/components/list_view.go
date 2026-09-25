package components

import (
	"cmp"
	"slices"
	"strings"

	"github.com/mmcdole/kino/internal/domain"
	"github.com/sahilm/fuzzy"
)

// listView is a list's view state: which items are shown, in what order, and
// which one is selected. It knows nothing about rendering.
type listView struct {
	items   []domain.ListItem
	visible []int // indexes into items: sorted, then narrowed by the filter
	cursor  int   // position in visible
	offset  int   // first visible position on screen
	rows    int   // how many positions fit on screen

	sortField SortField
	sortDir   SortDirection
	query     string
}

// setItems replaces the items, keeping the sort, the filter, and the selected
// item when it is still present; otherwise the cursor keeps its position.
func (v *listView) setItems(items []domain.ListItem) {
	selected := v.selected()
	cursor := v.cursor
	v.items = slices.Clone(items)
	v.rebuild()
	if selected == nil || !v.selectID(selected.GetID()) {
		v.moveTo(cursor)
	}
}

// sortBy orders the items and moves the cursor to the top.
func (v *listView) sortBy(field SortField, dir SortDirection) {
	v.sortField, v.sortDir = field, dir
	v.rebuild()
	v.moveTo(0)
}

// filter narrows the list to fuzzy title matches, best first, and moves the
// cursor to the best match. An empty query shows everything.
func (v *listView) filter(query string) {
	v.query = query
	v.rebuild()
	v.moveTo(0)
}

// rebuild recomputes visible from the items, sort and filter.
func (v *listView) rebuild() {
	keys := make([]itemPresentation, len(v.items))
	for i, item := range v.items {
		keys[i] = present(item)
	}
	order := make([]int, len(v.items))
	for i := range order {
		order[i] = i
	}
	if v.sortField != SortDefault {
		slices.SortStableFunc(order, func(a, b int) int {
			c := compareBy(v.sortField, v.items[a], v.items[b], keys[a], keys[b])
			if v.sortDir == SortDesc {
				return -c
			}
			return c
		})
	}
	if v.query == "" {
		v.visible = order
		return
	}
	titles := make([]string, len(order))
	for i, idx := range order {
		titles[i] = strings.ToLower(keys[idx].Title)
	}
	matches := fuzzy.Find(strings.ToLower(v.query), titles)
	v.visible = make([]int, len(matches))
	for i, match := range matches {
		v.visible[i] = order[match.Index]
	}
}

func compareBy(field SortField, a, b domain.ListItem, ka, kb itemPresentation) int {
	switch field {
	case SortTitle:
		return cmp.Compare(strings.ToLower(ka.SortTitle), strings.ToLower(kb.SortTitle))
	case SortDateAdded:
		return cmp.Compare(ka.AddedAt, kb.AddedAt)
	case SortLastUpdated:
		return cmp.Compare(ka.UpdatedAt, kb.UpdatedAt)
	case SortReleased:
		return cmp.Compare(ka.Year, kb.Year)
	case SortDuration:
		return cmp.Compare(ka.Duration, kb.Duration)
	case SortRating:
		return cmp.Compare(ka.Rating, kb.Rating)
	case SortEpisodeNum:
		ea, okA := a.(*domain.MediaItem)
		eb, okB := b.(*domain.MediaItem)
		if !okA || !okB {
			return 0
		}
		return cmp.Or(cmp.Compare(ea.SeasonNum, eb.SeasonNum), cmp.Compare(ea.EpisodeNum, eb.EpisodeNum))
	}
	return 0
}

func (v *listView) len() int { return len(v.visible) }

func (v *listView) at(pos int) domain.ListItem { return v.items[v.visible[pos]] }

func (v *listView) selected() domain.ListItem {
	if v.cursor >= len(v.visible) {
		return nil
	}
	return v.at(v.cursor)
}

// selectID moves the cursor to the item with id, if it is shown.
func (v *listView) selectID(id string) bool {
	for pos, idx := range v.visible {
		if v.items[idx].GetID() == id {
			v.moveTo(pos)
			return true
		}
	}
	return false
}

// moveTo puts the cursor at pos, clamped to the list, and scrolls to it.
func (v *listView) moveTo(pos int) {
	v.cursor = max(0, min(pos, len(v.visible)-1))
	v.scroll()
}

func (v *listView) move(delta int) { v.moveTo(v.cursor + delta) }

// setRows sets how many positions fit on screen.
func (v *listView) setRows(rows int) {
	v.rows = max(1, rows)
	v.scroll()
}

func (v *listView) scroll() {
	if v.rows == 0 {
		return
	}
	v.offset = max(0, min(v.offset, v.cursor, len(v.visible)-v.rows))
	if v.cursor >= v.offset+v.rows {
		v.offset = v.cursor - v.rows + 1
	}
}
