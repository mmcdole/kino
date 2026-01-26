package domain

import "errors"

// Sentinel errors for domain operations
var (
	// ErrItemNotFound indicates the requested media item does not exist
	ErrItemNotFound = errors.New("media item not found")

	// ErrServerOffline indicates the media server is unreachable
	ErrServerOffline = errors.New("media server is unreachable")

	// ErrAuthFailed indicates authentication failed
	ErrAuthFailed = errors.New("authentication token is invalid")

	// ErrLibraryNotFound indicates the requested library does not exist
	ErrLibraryNotFound = errors.New("library not found")
)
