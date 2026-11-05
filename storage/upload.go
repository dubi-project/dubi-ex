package storage

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"strings"

	"github.com/dubi-project/dubi-ex/internal/support"
	. "github.com/dubi-project/dubi-go/base"
)

func Upload(from any, opts ...UploadOption) (*File, error) { return UploadTo("", from, opts...) }
func UploadTo(source string, from any, opts ...UploadOption) (*File, error) {
	var name string
	switch value := from.(type) {
	case string:
		name = value
	case Map:
		name, _ = value["file"].(string)
	case map[string]any:
		name, _ = value["file"].(string)
	default:
		return nil, ErrInvalidOption
	}
	if name == "" {
		return nil, ErrInvalidOption
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	opt := UploadOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Name == "" {
		opt.Name = name
	}
	if opt.Size != nil && *opt.Size < 0 {
		return nil, ErrInvalidOption
	}
	if opt.Size != nil && *opt.Size != info.Size() {
		return nil, ErrSizeMismatch
	}
	size := info.Size()
	opt.Size = &size
	return module.upload(source, file, opt)
}
func UploadReader(reader io.Reader, opts ...UploadOption) (*File, error) {
	return UploadReaderTo("", reader, opts...)
}
func UploadReaderTo(source string, reader io.Reader, opts ...UploadOption) (*File, error) {
	opt := UploadOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	return module.upload(source, reader, opt)
}

type countedReader struct {
	io.Reader
	n    int64
	size *int64
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	if r.size != nil && (r.n > *r.size || (err == io.EOF && r.n != *r.size)) {
		return n, ErrSizeMismatch
	}
	return n, err
}

func (m *Module) upload(source string, reader io.Reader, opt UploadOption) (*File, error) {
	if reader == nil {
		return nil, ErrInvalidOption
	}
	ctx, release, err := m.begin(opt.Context)
	if err != nil {
		return nil, err
	}
	defer release()
	opt.Context = ctx
	if source != "" && m.instances[source] == nil {
		return nil, ErrInvalidConnection
	}
	if opt.Size != nil {
		size := *opt.Size
		if size < 0 {
			return nil, ErrInvalidOption
		}
		opt.Size = &size
	}
	opt.Prefix = strings.TrimRight(opt.Prefix, "/")
	if !validRelative(opt.Prefix, true) {
		return nil, ErrInvalidPath
	}
	if opt.Type == "" && opt.Mimetype != "" {
		media, _, _ := mime.ParseMediaType(opt.Mimetype)
		switch media {
		case "image/jpeg":
			opt.Type = "jpg"
		case "image/png":
			opt.Type = "png"
		case "image/gif":
			opt.Type = "gif"
		case "image/webp":
			opt.Type = "webp"
		default:
			extensions, _ := mime.ExtensionsByType(media)
			if len(extensions) > 0 {
				opt.Type = strings.TrimPrefix(extensions[0], ".")
			}
		}
	}
	if opt.Type == "" {
		opt.Type = extension(opt.Name)
	}
	opt.Type = strings.TrimPrefix(strings.ToLower(opt.Type), ".")
	if !validType(opt.Type) {
		return nil, ErrInvalidPath
	}
	opt.Metadata, opt.Tags = support.CloneMap(opt.Metadata), support.CloneMap(opt.Tags)
	if !validRelative(opt.Key, true) {
		return nil, ErrInvalidPath
	}
	if source == "" {
		route := opt.Key
		if route == "" {
			route = opt.Name
		}
		source = m.ring.Locate(path.Join(opt.Prefix, route))
	}
	inst := m.instances[source]
	if inst == nil {
		return nil, ErrInvalidConnection
	}
	opt.Prefix = path.Join(inst.Config.Prefix, opt.Prefix)
	if opt.Prefix == "." {
		opt.Prefix = ""
	}
	if err := validateUploadOptions(inst.conn, opt); err != nil {
		return nil, err
	}
	if preparer, ok := inst.conn.(UploadPreparer); ok {
		prepared, resolved, err := preparer.PrepareUpload(reader, opt)
		if err != nil {
			if prepared != nil {
				_ = prepared.Close()
			}
			return nil, err
		}
		if prepared == nil {
			return nil, ErrInvalidOption
		}
		defer prepared.Close()
		if (opt.Key != "" && resolved.Key != opt.Key) || resolved.Type != opt.Type ||
			(opt.Prefix != "" && resolved.Prefix != opt.Prefix && !strings.HasPrefix(resolved.Prefix, opt.Prefix+"/")) {
			return nil, fmt.Errorf("storage: driver changed upload location: %w", ErrInvalidOption)
		}
		// Preparation resolves location and size; the operation's context and
		// caller's declared size remain authoritative.
		resolved.Context = ctx
		if opt.Size != nil {
			if resolved.Size != nil && *resolved.Size != *opt.Size {
				return nil, ErrSizeMismatch
			}
			resolved.Size = opt.Size
		}
		opt, reader = resolved, prepared
	}
	if opt.Key == "" {
		return nil, fmt.Errorf("storage: driver must resolve an upload key: %w", ErrInvalidOption)
	}
	if !validRelative(opt.Key, false) || !validRelative(opt.Prefix, true) || !validType(opt.Type) {
		return nil, ErrInvalidPath
	}
	if opt.Size != nil && *opt.Size < 0 {
		return nil, ErrInvalidOption
	}
	file := inst.NewFile(opt.Prefix, opt.Key, opt.Type, 0)
	unlock := m.objects.lock(objectID(file), true)
	defer unlock()
	counted := &countedReader{Reader: contextReader{ctx, reader}, size: opt.Size}
	stored, err := inst.conn.Upload(counted, opt)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if opt.Size != nil && counted.n != *opt.Size {
		return nil, fmt.Errorf("storage: driver did not consume declared upload size: %w", ErrSizeMismatch)
	}
	if stored == nil || stored.base != file.base || stored.File() != file.File() || stored.size != counted.n {
		return nil, fmt.Errorf("storage: driver returned inconsistent file metadata")
	}
	file.size, file.hash, file.version = counted.n, stored.hash, stored.version
	if err = m.invalidate(file); err != nil {
		return file, fmt.Errorf("storage: object committed, cache invalidation failed: %w", err)
	}
	return file, nil
}
