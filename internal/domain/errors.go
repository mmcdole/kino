package domain

import "errors"

// Sentinel errors for domain operations
var (
	// ErrItemNotFound indicates the requested media item does not exist
	ErrItemNotFound = errors.New("media item not found")

	// ErrServerOffline indicates the media server is unreachable
	ErrServerOffline = errors.New("media server is unreachable")

	// ErrPlayerCrashed indicates the external player process crashed
	ErrPlayerCrashed = errors.New("external player process crashed")

	// ErrPlayerNotRunning indicates the player is not currently running
	ErrPlayerNotRunning = errors.New("player is not running")

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

	// ErrPINExpired indicates the authentication PIN has expired
	ErrPINExpired = errors.New("authentication PIN has expired")

	// ErrPINNotClaimed indicates the PIN has not been claimed yet
	ErrPINNotClaimed = errors.New("authentication PIN not yet claimed")

	// ErrSocketConnection indicates a socket connection failure
	ErrSocketConnection = errors.New("failed to connect to socket")

	// ErrInvalidResponse indicates an unexpected response from the server
	ErrInvalidResponse = errors.New("invalid response from server")

	// ErrPlaybackFailed indicates playback could not be started
	ErrPlaybackFailed = errors.New("failed to start playback")
)
