package media

// CloneItems copies entity values so consumers can retain or modify snapshots
// independently of other requests and projections.
func CloneItems(items []ListItem) []ListItem {
	out := make([]ListItem, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case *Item:
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

// LibraryItems widens a slice of libraries, pointing into the original slice.
func LibraryItems(libs []Library) []ListItem {
	items := make([]ListItem, len(libs))
	for i := range libs {
		items[i] = &libs[i]
	}
	return items
}
