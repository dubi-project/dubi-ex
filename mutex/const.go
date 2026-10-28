package mutex

import "errors"

var (
	ErrInvalidConnection  = errors.New("invalid mutex connection")
	ErrNotReady           = errors.New("mutex is not ready")
	ErrClosed             = errors.New("mutex is closed")
	ErrLocked             = errors.New("mutex already locked")
	ErrLostLock           = errors.New("mutex lock is lost")
	ErrTimeout            = errors.New("mutex timeout")
	ErrInvalidLease       = errors.New("invalid mutex lease")
	ErrUnsupportedCheck   = errors.New("mutex locked check is unsupported")
	ErrUnsupportedRefresh = errors.New("mutex refresh is unsupported")
	ErrTokenRequired      = errors.New("mutex unlock requires token")
)
