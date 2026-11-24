// Package menu turns the library into lines for a dmenu-style launcher
// (vicinae dmenu, fuzzel --dmenu, rofi -dmenu, ...) and back: the launcher
// filters the lines and returns the chosen one, which Find resolves to the
// movie or show it names.
package menu

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mmcdole/kino/internal/media"
)

// Entry is one line of the menu and the title it names.
type Entry struct {
	Label   string
	Item    media.ListItem // *media.Item (a movie) or *media.Show
	Library media.Library
}

// Library is one library and its contents, in the order they are listed.
type Library struct {
	Library media.Library
	Items   []media.ListItem
}

// Entries lists every movie and show, one line each, sorted by label. Labels
// are unique, so the line a launcher returns names exactly one title: a
// title in two libraries gets its library's name, and anything still alike
// gets a number.
func Entries(libraries []Library) []Entry {
	var entries []Entry
	for _, lib := range libraries {
		for _, item := range lib.Items {
			if label, ok := baseLabel(item); ok {
				entries = append(entries, Entry{Label: label, Item: item, Library: lib.Library})
			}
		}
	}
	disambiguate(entries, func(e Entry) string { return e.Library.Name })
	disambiguate(entries, nil)
	slices.SortStableFunc(entries, func(a, b Entry) int {
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return entries
}

// baseLabel is "Title (Year)" for a movie and "Title (Year) [TV]" for a show.
func baseLabel(item media.ListItem) (string, bool) {
	var title, suffix string
	var year int
	switch v := item.(type) {
	case *media.Item:
		if v.Type != media.TypeMovie {
			return "", false
		}
		title, year = v.Title, v.Year
	case *media.Show:
		title, year, suffix = v.Title, v.Year, " [TV]"
	default:
		return "", false
	}
	// A launcher returns one line, so a title can't span two.
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return "", false
	}
	if year > 0 {
		title = fmt.Sprintf("%s (%d)", title, year)
	}
	return title + suffix, true
}

// disambiguate appends extra(e) to every label shared by more than one
// entry, or a running number when extra is nil.
func disambiguate(entries []Entry, extra func(Entry) string) {
	count := make(map[string]int)
	for _, e := range entries {
		count[e.Label]++
	}
	seen := make(map[string]int)
	for i, e := range entries {
		if count[e.Label] < 2 {
			continue
		}
		seen[e.Label]++
		if extra == nil {
			entries[i].Label = fmt.Sprintf("%s #%d", e.Label, seen[e.Label])
		} else if s := extra(e); s != "" {
			entries[i].Label = e.Label + " - " + s
		}
	}
}

// Find returns the entry a launcher's output names. Launchers return the
// line as listed, though some keep trailing whitespace or a newline.
func Find(entries []Entry, line string) (Entry, bool) {
	line = strings.TrimSpace(line)
	for _, e := range entries {
		if e.Label == line {
			return e, true
		}
	}
	return Entry{}, false
}

// Season is one season of a show and its episodes.
type Season struct {
	Season   *media.Season
	Episodes []*media.Item
}

// NextEpisode picks what playing a show means: the first episode in progress
// or not yet watched, in season and episode order with specials last. When
// everything has been watched it starts the show over.
func NextEpisode(seasons []Season) (*media.Item, bool) {
	all := sortedEpisodes(seasons)
	if len(all) == 0 {
		return nil, false
	}
	for _, ep := range all {
		if !ep.IsPlayed {
			return ep, true
		}
	}
	return all[0], true
}

// Episodes lists a show's episodes for a launcher, in order with specials
// last, behind an "Up next" line for NextEpisode's pick. Each line shows
// whether the episode is watched (✓) or in progress (◐).
func Episodes(seasons []Season) []Entry {
	all := sortedEpisodes(seasons)
	var entries []Entry
	if next, ok := NextEpisode(seasons); ok {
		entries = append(entries, Entry{Label: "▶ Up next: " + episodeLabel(next), Item: next})
	}
	for _, ep := range all {
		label := episodeLabel(ep)
		switch ep.WatchStatus() {
		case media.WatchStatusWatched:
			label += "  ✓"
		case media.WatchStatusInProgress:
			label += "  ◐"
		}
		entries = append(entries, Entry{Label: label, Item: ep})
	}
	disambiguate(entries, nil)
	return entries
}

// episodeLabel is "S01E02  Title".
func episodeLabel(ep *media.Item) string {
	title := strings.Join(strings.Fields(ep.Title), " ")
	return fmt.Sprintf("S%02dE%02d  %s", ep.SeasonNum, ep.EpisodeNum, title)
}

// sortedEpisodes is every episode in season and episode order, specials last.
func sortedEpisodes(seasons []Season) []*media.Item {
	var all []*media.Item
	for _, s := range seasons {
		all = append(all, s.Episodes...)
	}
	slices.SortStableFunc(all, func(a, b *media.Item) int {
		if (a.SeasonNum == 0) != (b.SeasonNum == 0) {
			if a.SeasonNum == 0 {
				return 1
			}
			return -1
		}
		if a.SeasonNum != b.SeasonNum {
			return a.SeasonNum - b.SeasonNum
		}
		return a.EpisodeNum - b.EpisodeNum
	})
	return all
}
