package domain

// ProgressFunc reports download progress to TUI.
// Called repeatedly during pagination: (50, 500), (100, 500), ...
type ProgressFunc func(loaded, total int)
