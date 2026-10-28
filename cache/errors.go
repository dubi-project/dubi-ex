package cache

import "errors"

const defaultName = "default"

var (
	ErrNotOpen           = errors.New("cache: module is not open")
	ErrInvalidConnection = errors.New("cache: invalid connection")
	ErrUnsafeClear       = errors.New("cache: empty-prefix clear requires ClearAll or allow_clear_all")
	ErrSequenceOverflow  = errors.New("cache: sequence overflows int64")
	ErrInvalidCount      = errors.New("cache: sequence batch count exceeds 1048576")
)
