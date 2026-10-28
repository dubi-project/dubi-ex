package storage

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type fileDriver struct{}
type fileConnection struct {
	instance  *Instance
	directory string
	publicURL string
	root      *os.Root
}

func (*fileDriver) Connect(inst *Instance) (Connection, error) {
	dir := filepath.Join("store", "storage", inst.Name)
	for _, key := range []string{"storage", "root"} {
		if value, ok := inst.Settings[key]; ok {
			text, ok := value.(string)
			if !ok || text == "" {
				return nil, ErrInvalidOption
			}
			dir = text
		}
	}
	publicURL := ""
	for _, key := range []string{"url", "base_url"} {
		if value, ok := inst.Settings[key]; ok {
			text, ok := value.(string)
			if !ok {
				return nil, ErrInvalidOption
			}
			publicURL = text
		}
	}
	if publicURL != "" {
		parsed, err := url.Parse(publicURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, ErrInvalidOption
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	return &fileConnection{instance: inst, directory: abs, publicURL: publicURL}, nil
}
func (c *fileConnection) Open() error {
	if err := os.MkdirAll(c.directory, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(c.directory)
	if err != nil {
		return err
	}
	c.root = root
	return nil
}
func (c *fileConnection) Close() error {
	if c.root == nil {
		return nil
	}
	err := c.root.Close()
	c.root = nil
	return err
}
func (c *fileConnection) Upload(reader io.Reader, opt UploadOption) (*File, error) {
	if !validRelative(opt.Prefix, true) || !validRelative(opt.Key, false) || !validType(opt.Type) {
		return nil, ErrInvalidPath
	}
	file := c.instance.NewFile(opt.Prefix, opt.Key, opt.Type, 0)
	n, err := atomicRootWrite(opt.Context, c.root, file.File(), reader)
	if err != nil {
		return nil, err
	}
	file.size = n
	return file, nil
}

type rangeFileReader struct {
	file *os.File
	*io.SectionReader
}

func (r *rangeFileReader) Close() error { return r.file.Close() }
func (c *fileConnection) Fetch(file *File, opt FetchOption) (Stream, error) {
	if err := opt.Context.Err(); err != nil {
		return nil, err
	}
	body, err := c.root.Open(file.File())
	if err != nil {
		return nil, err
	}
	info, err := body.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = body.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalidPath
	}
	if opt.Range == nil {
		return body, nil
	}
	start, end := opt.Range.Start, opt.Range.End
	size := info.Size()
	if start < 0 || end < -1 || start >= size || (end >= 0 && end < start) {
		_ = body.Close()
		return nil, ErrInvalidRange
	}
	if end == -1 || end >= size {
		end = size - 1
	}
	return &rangeFileReader{file: body, SectionReader: io.NewSectionReader(body, start, end-start+1)}, nil
}
func (c *fileConnection) Remove(file *File, opt RemoveOption) error {
	if err := opt.Context.Err(); err != nil {
		return err
	}
	err := c.root.Remove(file.File())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func (c *fileConnection) Stat(file *File, opt StatOption) (ObjectInfo, error) {
	if err := opt.Context.Err(); err != nil {
		return ObjectInfo{}, err
	}
	info, err := c.root.Stat(file.File())
	if err != nil {
		return ObjectInfo{}, err
	}
	if !info.Mode().IsRegular() {
		return ObjectInfo{}, ErrInvalidPath
	}
	return ObjectInfo{Size: info.Size(), Version: fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size()), Modified: info.ModTime()}, nil
}
func (c *fileConnection) Local(file *File, opt LocalOption) (string, error) {
	if _, err := c.Stat(file, StatOption{Context: opt.Context}); err != nil {
		return "", err
	}
	return filepath.Join(c.directory, filepath.FromSlash(file.File())), nil
}
func (c *fileConnection) Browse(file *File, opt BrowseOption) (string, error) {
	if c.publicURL == "" {
		return "", ErrBrowseUnsupported
	}
	if _, err := c.Stat(file, StatOption{Context: opt.Context}); err != nil {
		return "", err
	}
	parsed, _ := url.Parse(c.publicURL)
	parsed.Path = path.Join(parsed.Path, file.File())
	parsed.RawPath = ""
	values := parsed.Query()
	for key, value := range opt.Params {
		values.Set(key, fmt.Sprint(value))
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}
func extension(name string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
}
