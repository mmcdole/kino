package components

// CollectionSummary describes the last accepted complete snapshot.
// Known distinguishes an empty collection from one that has not loaded.
type CollectionSummary struct {
	Count int
	Known bool
	Stale bool
}

// LoadActivity describes visible network activity, independently of content.
type LoadActivity struct {
	Visible bool
	Loaded  int
	Total   int
}

// CollectionFeedback is derived from content and all live subscribers.
type CollectionFeedback struct {
	Pending  bool
	Summary  CollectionSummary
	Activity LoadActivity
	Error    error
}
