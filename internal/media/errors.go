package media

import "errors"

// Sentinel errors shared by the backends and the catalog
var (
	// ErrItemNotFound indicates the requested media item does not exist
	ErrItemNotFound = errors.New("media item not found")

	// ErrServerOffline indicates the media server is unreachable
	ErrServerOffline = errors.New("media server is unreachable")

	// ErrAuthFailed indicates the server rejected our token (revoked or expired)
	ErrAuthFailed = errors.New("authentication token is invalid or expired")
)
