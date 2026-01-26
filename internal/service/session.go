package service

import "github.com/mmcdole/kino/internal/adapter"

// SessionService manages user session operations
type SessionService struct{}

// NewSessionService creates a new SessionService
func NewSessionService() *SessionService {
	return &SessionService{}
}

// Logout clears server configuration and cached data
func (s *SessionService) Logout() error {
	// Clear server configuration
	if err := adapter.ClearServerConfig(); err != nil {
		return err
	}

	// Clear cache
	if err := adapter.ClearCache(); err != nil {
		return err
	}

	return nil
}
