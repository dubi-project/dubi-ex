package cache

import "time"

// Driver creates an independent connection for one configured source.
type Driver interface {
	Connect(*Instance) (Connect, error)
}

// Connect operates on already-prefixed keys and encoded bytes. Implementations
// must support concurrent calls. A miss returns (nil, nil); a stored empty value
// returns a non-nil empty slice. Non-positive TTLs mean no expiration.
// Sequence returns start for a missing key and otherwise adds step atomically.
type Connect interface {
	Open() error
	Close() error
	Read(string) ([]byte, error)
	Write(string, []byte, time.Duration) error
	Exists(string) (bool, error)
	Delete(string) error
	Sequence(key string, start, step int64, expire time.Duration) (int64, error)
	Keys(prefix string) ([]string, error)
	Clear(prefix string) error
}

// ManyConnect optionally reserves a batch atomically. Without this interface,
// SequenceMany falls back to individual Sequence calls, which may interleave
// with calls from other goroutines or processes.
type ManyConnect interface {
	SequenceMany(key string, start, step, count int64, expire time.Duration) ([]int64, error)
}
