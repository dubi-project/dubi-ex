package storage

import "mime/multipart"

func uploadMultipartTo(source string, header *multipart.FileHeader, opt UploadOption) (*File, error) {
	if header == nil || header.Size < 0 || (opt.Size != nil && *opt.Size < 0) {
		return nil, ErrInvalidOption
	}
	if opt.Size != nil && *opt.Size != header.Size {
		return nil, ErrSizeMismatch
	}
	if opt.Name == "" {
		opt.Name = header.Filename
	}
	// Browsers often send application/octet-stream; preserve the filename's
	// extension unless the caller explicitly supplies Type or Mimetype.
	if opt.Type == "" && opt.Mimetype == "" {
		opt.Type = extension(opt.Name)
	}
	if opt.Mimetype == "" {
		opt.Mimetype = header.Header.Get("Content-Type")
	}
	size := header.Size
	opt.Size = &size
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return module.upload(source, file, opt)
}
