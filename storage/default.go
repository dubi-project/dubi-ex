package storage

import (
	"crypto/sha1"
	"encoding/hex"
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
	temp      string
	keyMode   KeyMode
	root      *os.Root
}

func (*fileDriver) Connect(inst *Instance) (Connection, error) {
	for key := range inst.Settings {
		switch key {
		case "root", "base_url", "key_mode", "temp":
		default:
			return nil, fmt.Errorf("storage: unknown local setting %q: %w", key, ErrInvalidOption)
		}
	}
	dir := filepath.Join("store", "storage", inst.Name)
	if value, ok := inst.Settings["root"]; ok {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, ErrInvalidOption
		}
		dir = text
	}
	publicURL := ""
	if value, ok := inst.Settings["base_url"]; ok {
		text, ok := value.(string)
		if !ok {
			return nil, ErrInvalidOption
		}
		publicURL = text
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
	mode := HashKey
	if value, ok := inst.Settings["key_mode"]; ok {
		text, ok := value.(string)
		if !ok || (KeyMode(text) != HashKey && KeyMode(text) != RandomKey) {
			return nil, fmt.Errorf("storage: invalid settings.key_mode: %w", ErrInvalidOption)
		}
		mode = KeyMode(text)
	}
	temp := filepath.Join("store", "temp", inst.Name)
	if value, ok := inst.Settings["temp"]; ok {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("storage: invalid settings.temp: %w", ErrInvalidOption)
		}
		temp = text
	}
	temp, err = filepath.Abs(temp)
	if err != nil {
		return nil, err
	}
	return &fileConnection{instance: inst, directory: abs, publicURL: publicURL, temp: temp, keyMode: mode}, nil
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
	if err := unsupportedUploadOptions(opt); err != nil {
		return nil, err
	}
	if !validRelative(opt.Prefix, true) || !validRelative(opt.Key, false) || !validType(opt.Type) {
		return nil, ErrInvalidPath
	}
	file := c.instance.NewFile(opt.Prefix, opt.Key, opt.Type, 0)
	h := sha1.New()
	var n int64
	info, err := atomicRootGenerateInfo(opt.Context, c.root, file.File(), func(writer io.Writer) error {
		var err error
		n, err = io.Copy(io.MultiWriter(writer, h), contextReader{opt.Context, reader})
		return err
	})
	if err != nil {
		return nil, err
	}
	file.size = n
	file.hash = hex.EncodeToString(h.Sum(nil))
	file.version = localVersion(info)
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
	return ObjectInfo{Size: info.Size(), Version: localVersion(info), Modified: info.ModTime()}, nil
}
func localVersion(info os.FileInfo) string {
	return fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
}
func (c *fileConnection) Local(file *File, opt LocalOption) (string, error) {
	if _, err := c.Stat(file, StatOption{Context: opt.Context}); err != nil {
		return "", err
	}
	return filepath.Join(c.directory, filepath.FromSlash(file.File())), nil
}
func (c *fileConnection) Browse(file *File, opt BrowseOption) (string, error) {
	if err := unsupportedBrowseOptions(opt); err != nil {
		return "", err
	}
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
