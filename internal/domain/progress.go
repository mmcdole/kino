package domain

// ProgressFunc reports download progress to TUI.
// Called repeatedly during pagination: (50, 500), (100, 500), ...
type ProgressFunc func(loaded, total int)

// SyncResult summarizes what happened during a sync operation.
type SyncResult struct {
	LibraryID string // Which library this result is for
	FromCache bool   // true if cache was fresh (no network fetch)
	Count     int    // total items after sync
}
