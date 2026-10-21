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

// ErrPlaybackRefused matches a RefusedError.
var ErrPlaybackRefused = errors.New("playback refused")

// RefusedError is a server declining to start a play for a reason the user
// can act on, such as a stream limit. Its text is written for the user.
type RefusedError struct {
	Message string
}

func (e *RefusedError) Error() string        { return e.Message }
func (e *RefusedError) Is(target error) bool { return target == ErrPlaybackRefused }

// ErrNoFallback is a play that failed which the server can't retry another way.
var ErrNoFallback = errors.New("playback failed and the server has nothing else to try")
