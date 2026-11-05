package storage

import (
	"errors"
	"fmt"
)

var (
	ErrNotOpen           = errors.New("storage: not open")
	ErrClosed            = errors.New("storage: closed")
	ErrInvalidConnection = errors.New("storage: invalid source")
	ErrInvalidCode       = errors.New("storage: invalid file code")
	ErrInvalidPath       = errors.New("storage: invalid object path")
	ErrInvalidRange      = errors.New("storage: invalid range")
	ErrInvalidOption     = errors.New("storage: invalid option")
	ErrInvalidHandler    = errors.New("storage: no matching processor")
	ErrUnsupported       = errors.New("storage: unsupported capability or option")
	ErrBrowseUnsupported = fmt.Errorf("storage: URL browsing: %w", ErrUnsupported)
	ErrLocalUnsupported  = fmt.Errorf("storage: local path: %w", ErrUnsupported)
	ErrStatUnsupported   = fmt.Errorf("storage: metadata lookup: %w", ErrUnsupported)
	ErrSizeMismatch      = errors.New("storage: upload size does not match declared size")
	ErrImageTooLarge     = errors.New("storage: image dimensions exceed limit")
)
