package components

// LibraryStatus represents the sync status of a library
type LibraryStatus int

const (
	StatusIdle LibraryStatus = iota
	StatusSyncing
	StatusSynced
	StatusError
)

// LibrarySyncState tracks sync progress for a single library
type LibrarySyncState struct {
	Status   LibraryStatus
	Loaded   int   // Items loaded so far
	Total    int   // Total items expected
	FromDisk bool  // Whether loaded from cache
	Error    error // Error if any
}
