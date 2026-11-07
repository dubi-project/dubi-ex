package storage

import (
	"fmt"
	"mime/multipart"
)

// Uploads accepts []*multipart.FileHeader or []multipart.FileHeader. Files are
// uploaded in input order. On the first error it returns the stored files and
// the error, without rolling back those files or attempting subsequent files.
func Uploads(from any, opts ...UploadOption) ([]*File, error) {
	return UploadsTo("", from, opts...)
}

// UploadsTo uploads all files to the named connection. Options apply to every
// file; a shared explicit Key is rejected for batches containing multiple files.
func UploadsTo(source string, from any, opts ...UploadOption) ([]*File, error) {
	var headers []*multipart.FileHeader
	switch values := from.(type) {
	case []*multipart.FileHeader:
		headers = values
	case []multipart.FileHeader:
		headers = make([]*multipart.FileHeader, len(values))
		for i := range values {
			headers[i] = &values[i]
		}
	default:
		return nil, ErrInvalidOption
	}
	opt := UploadOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if len(headers) > 1 && opt.Key != "" {
		return nil, fmt.Errorf("storage: multiple uploads cannot share an explicit key: %w", ErrInvalidOption)
	}
	files := make([]*File, 0, len(headers))
	for i, header := range headers {
		file, err := uploadMultipartTo(source, header, opt)
		// Upload may return a committed file with a subsequent cache error.
		if file != nil {
			files = append(files, file)
		}
		if err != nil {
			return files, fmt.Errorf("storage: upload at index %d: %w", i, err)
		}
	}
	return files, nil
}
