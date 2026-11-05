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
// Size, when non-nil, is the expected byte count. Read to EOF and propagate read
// errors (including ErrSizeMismatch) before committing the object.
type Connection interface {
	Open() error
	Close() error
	Upload(io.Reader, UploadOption) (*File, error)
	Fetch(*File, FetchOption) (Stream, error)
	Remove(*File, RemoveOption) error
}

// UploadPreparer resolves driver-specific object naming before the module locks
// and writes the object. It must preserve an explicit Key and the supplied
// connection Prefix. The returned reader must reproduce the complete content;
// its Close releases preparation resources, never the caller's original reader.
// Drivers without this interface require an explicit upload Key.
type UploadPreparer interface {
	PrepareUpload(io.Reader, UploadOption) (io.ReadCloser, UploadOption, error)
}

// UploadValidator opts into advanced upload options. ValidateUpload must reject
// options the driver cannot honor, without reading or writing object data.
// Without this interface, Metadata, Tags and Expires are rejected by the module.
type UploadValidator interface {
	ValidateUpload(UploadOption) error
}

// BrowseValidator opts into signed or header-dependent URLs. Without it, the
// module rejects Expires and Headers; Params remain ordinary URL query values.
type BrowseValidator interface {
	ValidateBrowse(BrowseOption) error
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
	Hash     string // Optional content digest; algorithm is defined by the driver.
	Version  string
	Modified time.Time
	Mimetype string
}

type KeyMode string

const (
	HashKey   KeyMode = "hash"
	RandomKey KeyMode = "random"
)

type Instance struct {
	Name     string
	Config   Config
	Settings Map
	conn     Connection
}

type UploadOption struct {
	Context  context.Context
	Key      string
	KeyMode  KeyMode // Driver-specific naming policy; the local driver supports hash/random.
	Size     *int64  // nil means unknown, a pointer to zero means an empty object.
	Prefix   string
	Name     string
	Type     string
	Mimetype string
	Metadata Map
	Tags     Map
	Expires  time.Time // HTTP cache expiry metadata, not automatic object deletion.
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
	Headers Map // Required request headers for a URL signed with these headers.
	Params  Map
	Expires time.Duration // Positive values require an expiring access URL.
}
type StatOption struct{ Context context.Context }
