package storage

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	n int64
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
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
	if opt.Key == "" {
		// Unknown-length readers and path uploads are snapshotted before using
		// the content hash as identity, avoiding source changes between passes.
		temp, e := os.CreateTemp(m.options.Temp, "upload-*")
		if e != nil {
			return nil, e
		}
		defer func() { _ = temp.Close(); _ = os.Remove(temp.Name()) }()
		h := sha256.New()
		if _, err = io.Copy(io.MultiWriter(temp, h), contextReader{ctx, reader}); err != nil {
			return nil, err
		}
		sum := h.Sum(nil)
		opt.Key = base64.RawURLEncoding.EncodeToString(sum)
		hexsum := hex.EncodeToString(sum)
		opt.Prefix = path.Join(opt.Prefix, hexsum[:2], hexsum[2:4])
		if _, err = temp.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		reader = temp
	}
	if !validRelative(opt.Key, false) {
		return nil, ErrInvalidPath
	}
	if source == "" {
		source = m.ring.Locate(path.Join(opt.Prefix, opt.Key))
	}
	inst := m.instances[source]
	if inst == nil {
		return nil, ErrInvalidConnection
	}
	opt.Prefix = path.Join(inst.Config.Prefix, opt.Prefix)
	if opt.Prefix == "." {
		opt.Prefix = ""
	}
	file := inst.NewFile(opt.Prefix, opt.Key, opt.Type, 0)
	unlock := m.objects.lock(objectID(file), true)
	defer unlock()
	h := sha256.New()
	counted := &countedReader{Reader: io.TeeReader(contextReader{ctx, reader}, h)}
	stored, err := inst.conn.Upload(counted, opt)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if stored == nil || stored.base != file.base || stored.File() != file.File() || stored.size != counted.n {
		return nil, fmt.Errorf("storage: driver returned inconsistent file metadata")
	}
	file.size = counted.n
	file.version = hex.EncodeToString(h.Sum(nil))
	if err = m.invalidate(file); err != nil {
		return file, fmt.Errorf("storage: object committed, cache invalidation failed: %w", err)
	}
	return file, nil
}
