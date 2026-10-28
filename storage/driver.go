package storage

import (
	"context"
	"io"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

type Driver interface {
	Connect(*Instance) (Connection, error)
}

// Stream is a real streaming body. Seeking is an optional capability.
type Stream interface{ io.ReadCloser }
type RandomAccessStream interface {
	Stream
	io.Seeker
	io.ReaderAt
}

// Upload receives a resolved key, prefix, type, and cancellable Context. A
// successful call must have consumed the whole reader and published a complete
// object; interrupted writes must preserve the previously committed object.
type Connection interface {
	Open() error
	Close() error
	Upload(io.Reader, UploadOption) (*File, error)
	Fetch(*File, FetchOption) (Stream, error)
	Remove(*File, RemoveOption) error
}
type Statter interface {
	Stat(*File, StatOption) (ObjectInfo, error)
}
type Localer interface {
	Local(*File, LocalOption) (string, error)
}
type Browser interface {
	Browse(*File, BrowseOption) (string, error)
}
type Health struct{ Workload int64 }
type HealthProvider interface{ Health() Health }

// Version must change whenever the stored content changes. The module only
// reuses persistent download/thumbnail caches when a Statter supplies a version.
type ObjectInfo struct {
	Size     int64
	Version  string
	Modified time.Time
	Mimetype string
}

type Instance struct {
	Name     string
	Config   Config
	Settings Map
	conn     Connection
}

type UploadOption struct {
	Context  context.Context
	Key      string
	Prefix   string
	Name     string
	Type     string
	Mimetype string
	Metadata Map
	Tags     Map
	Expires  time.Time
}

// Range includes Start and End. End=-1 reads to EOF; nil Range reads the full
// object. In particular &Range{Start:0,End:0} reads exactly the first byte.
type Range struct{ Start, End int64 }
type FetchOption struct {
	Context context.Context
	Range   *Range
}
type DownloadOption struct {
	Context context.Context
	Target  string
}
type LocalOption struct{ Context context.Context }
type RemoveOption struct{ Context context.Context }
type BrowseOption struct {
	Context context.Context
	Headers Map
	Params  Map
	Expires time.Duration
}
type StatOption struct{ Context context.Context }
