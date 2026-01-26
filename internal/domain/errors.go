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

	// ErrAuthExpired indicates the authentication token has expired
	ErrAuthExpired = errors.New("authentication token has expired")

	// ErrLibraryNotFound indicates the requested library does not exist
	ErrLibraryNotFound = errors.New("library not found")

	// ErrSeasonNotFound indicates the requested season does not exist
	ErrSeasonNotFound = errors.New("season not found")

	// ErrShowNotFound indicates the requested show does not exist
	ErrShowNotFound = errors.New("show not found")

	// ErrNoNextEpisode indicates there is no next episode available
	ErrNoNextEpisode = errors.New("no next episode available")
)
