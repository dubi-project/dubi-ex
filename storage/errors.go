package storage

import "errors"

var (
	ErrNotOpen           = errors.New("storage: not open")
	ErrClosed            = errors.New("storage: closed")
	ErrInvalidConnection = errors.New("storage: invalid source")
	ErrInvalidCode       = errors.New("storage: invalid file code")
	ErrInvalidPath       = errors.New("storage: invalid object path")
	ErrInvalidRange      = errors.New("storage: invalid range")
	ErrInvalidOption     = errors.New("storage: invalid option")
	ErrInvalidHandler    = errors.New("storage: no matching processor")
	ErrBrowseUnsupported = errors.New("storage: URL browsing unsupported")
	ErrLocalUnsupported  = errors.New("storage: local path unsupported")
	ErrStatUnsupported   = errors.New("storage: metadata lookup unsupported")
	ErrImageTooLarge     = errors.New("storage: image dimensions exceed limit")
)
