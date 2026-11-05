package storage

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"io"
	"os"
	"path"
)

// Only the local driver uses content-addressed SHA-1 names and disk staging.
func (c *fileConnection) PrepareUpload(reader io.Reader, opt UploadOption) (io.ReadCloser, UploadOption, error) {
	if opt.Key != "" {
		return io.NopCloser(reader), opt, nil
	}
	mode := opt.KeyMode
	if mode == "" {
		mode = c.keyMode
	}
	switch mode {
	case RandomKey:
		var key [16]byte
		if _, err := rand.Read(key[:]); err != nil {
			return nil, opt, err
		}
		opt.Key = hex.EncodeToString(key[:])
		opt.Prefix = path.Join(opt.Prefix, opt.Key[:2], opt.Key[2:4])
		return io.NopCloser(reader), opt, nil
	case HashKey:
		return c.prepareHashUpload(reader, opt)
	default:
		return nil, opt, ErrInvalidOption
	}
}

type uploadSnapshot struct{ *os.File }

func (s *uploadSnapshot) Close() error {
	err := s.File.Close()
	removeErr := os.Remove(s.Name())
	if err != nil {
		return err
	}
	return removeErr
}

func (c *fileConnection) prepareHashUpload(reader io.Reader, opt UploadOption) (prepared io.ReadCloser, resolved UploadOption, err error) {
	if err = os.MkdirAll(c.temp, 0755); err != nil {
		return nil, opt, err
	}
	temp, err := os.CreateTemp(c.temp, "upload-*")
	if err != nil {
		return nil, opt, err
	}
	snapshot := &uploadSnapshot{temp}
	defer func() {
		if err != nil {
			_ = snapshot.Close()
		}
	}()
	h := sha1.New()
	size, err := io.Copy(io.MultiWriter(temp, h), &countedReader{Reader: contextReader{opt.Context, reader}, size: opt.Size})
	if err != nil {
		return nil, opt, err
	}
	if err = opt.Context.Err(); err != nil {
		return nil, opt, err
	}
	sum := h.Sum(nil)
	opt.Key = base64.RawURLEncoding.EncodeToString(sum)
	hexsum := hex.EncodeToString(sum)
	opt.Prefix = path.Join(opt.Prefix, hexsum[:2], hexsum[2:4])
	opt.Size = &size
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		return nil, opt, err
	}
	return snapshot, opt, nil
}
