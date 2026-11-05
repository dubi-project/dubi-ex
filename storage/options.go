package storage

import "fmt"

func unsupportedOption(operation, name string) error {
	return fmt.Errorf("storage: %s option %q: %w", operation, name, ErrUnsupported)
}

func unsupportedUploadOptions(opt UploadOption) error {
	if len(opt.Metadata) > 0 {
		return unsupportedOption("upload", "metadata")
	}
	if len(opt.Tags) > 0 {
		return unsupportedOption("upload", "tags")
	}
	if !opt.Expires.IsZero() {
		return unsupportedOption("upload", "expires")
	}
	return nil
}

func validateUploadOptions(conn Connection, opt UploadOption) error {
	if validator, ok := conn.(UploadValidator); ok {
		return validator.ValidateUpload(opt)
	}
	return unsupportedUploadOptions(opt)
}

func unsupportedBrowseOptions(opt BrowseOption) error {
	if opt.Expires < 0 {
		return ErrInvalidOption
	}
	if opt.Expires > 0 {
		return unsupportedOption("browse", "expires")
	}
	if len(opt.Headers) > 0 {
		return unsupportedOption("browse", "headers")
	}
	return nil
}

func validateBrowseOptions(conn Connection, opt BrowseOption) error {
	if opt.Expires < 0 {
		return ErrInvalidOption
	}
	if validator, ok := conn.(BrowseValidator); ok {
		return validator.ValidateBrowse(opt)
	}
	return unsupportedBrowseOptions(opt)
}
