package domain

import "slices"

// WatchChange marks one item played or unplayed. ShowID and SeasonID name the
// parents whose unwatched counts roll up from an episode; both are empty for
// movies.
type WatchChange struct {
	ItemID   string
	ShowID   string
	SeasonID string
	Played   bool
}

// IDs returns every entity ID the change can touch.
func (c WatchChange) IDs() []string {
	var ids []string
	for _, id := range []string{c.ItemID, c.ShowID, c.SeasonID} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// Apply updates every list that holds the item and returns only the lists it
// changed. Parent unwatched counts move by one, and only when some cached copy
// of the item actually changed state, so applying a change twice is harmless.
// Input slices and entities are never modified.
func (c WatchChange) Apply(lists map[string][]ListItem) map[string][]ListItem {
	changed := make(map[string][]ListItem)
	flipped := false
	for key, items := range lists {
		out := items
		for i, item := range items {
			m, ok := item.(*MediaItem)
			if !ok || m.ID != c.ItemID || (m.IsPlayed == c.Played && m.ViewOffset == 0) {
				continue
			}
			flipped = flipped || m.IsPlayed != c.Played
			patched := *m
			patched.IsPlayed, patched.ViewOffset = c.Played, 0
			if len(changed[key]) == 0 {
				out = slices.Clone(items)
			}
			out[i] = &patched
			changed[key] = out
		}
	}
	if !flipped || c.ShowID == "" {
		return changed
	}

	delta := 1
	if c.Played {
		delta = -1
	}
	for key, items := range lists {
		out, cloned := items, false
		if prior, ok := changed[key]; ok {
			out, cloned = prior, true
		}
		for i, item := range out {
			var patched ListItem
			switch v := item.(type) {
			case *Show:
				if v.ID == c.ShowID {
					s := *v
					s.UnwatchedCount = min(max(s.UnwatchedCount+delta, 0), s.EpisodeCount)
					patched = &s
				}
			case *Season:
				if v.ID == c.SeasonID {
					s := *v
					s.UnwatchedCount = min(max(s.UnwatchedCount+delta, 0), s.EpisodeCount)
					patched = &s
				}
			}
			if patched == nil {
				continue
			}
			if !cloned {
				out, cloned = slices.Clone(items), true
			}
			out[i] = patched
			changed[key] = out
		}
	}
	return changed
}
