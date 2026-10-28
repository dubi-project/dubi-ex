package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path"
	"strconv"
	"strings"
	"unicode"
)

type File struct {
	base, prefix, key, typee, version string
	size                              int64
	proxy, remote                     bool
	ctx                               context.Context
}

func (f *File) Base() string    { return f.base }
func (f *File) Prefix() string  { return f.prefix }
func (f *File) Key() string     { return f.key }
func (f *File) Type() string    { return f.typee }
func (f *File) Size() int64     { return f.size }
func (f *File) Version() string { return f.version }
func (f *File) Proxy() bool     { return f.proxy }
func (f *File) Remote() bool    { return f.remote }
func (f *File) File() string {
	name := f.key
	if f.typee != "" {
		name += "." + f.typee
	}
	return path.Join(f.prefix, name)
}
func (f *File) Name() string { return path.Base(f.File()) }

type fileReference struct {
	Base    string `json:"base"`
	Prefix  string `json:"prefix,omitempty"`
	Key     string `json:"key"`
	Type    string `json:"type,omitempty"`
	Size    int64  `json:"size"`
	Version string `json:"version,omitempty"`
}

func (f *File) Code() string {
	data, _ := json.Marshal(fileReference{f.base, f.prefix, f.key, f.typee, f.size, f.version})
	return "v1." + base64.RawURLEncoding.EncodeToString(data)
}
func (f *File) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code string `json:"code"`
		Name string `json:"name"`
		Size int64  `json:"size"`
		Type string `json:"type,omitempty"`
	}{f.Code(), f.Name(), f.size, f.typee})
}
func (i *Instance) NewFile(prefix, key, typee string, size int64) *File {
	return &File{base: i.Name, prefix: prefix, key: key, typee: typee, size: size, proxy: i.Config.Proxy, remote: i.Config.Remote}
}

func Decode(code string) (*File, error) {
	var ref fileReference
	if strings.HasPrefix(code, "v1.") {
		data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "v1."))
		if err != nil || json.Unmarshal(data, &ref) != nil {
			return nil, ErrInvalidCode
		}
	} else {
		// Read infrago's existing five-field references as well.
		data, err := base64.RawURLEncoding.DecodeString(code)
		if err != nil {
			return nil, ErrInvalidCode
		}
		fields := strings.Split(string(data), "\t")
		if len(fields) != 5 {
			return nil, ErrInvalidCode
		}
		size, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, ErrInvalidCode
		}
		ref = fileReference{Base: fields[0], Prefix: fields[1], Key: fields[2], Type: fields[3], Size: size}
	}
	if ref.Base == "" {
		ref.Base = "default"
	}
	if !validSource(ref.Base) || !validRelative(ref.Prefix, true) || !validRelative(ref.Key, false) || !validType(ref.Type) || ref.Size < 0 {
		return nil, ErrInvalidCode
	}
	return &File{base: ref.Base, prefix: ref.Prefix, key: ref.Key, typee: ref.Type, size: ref.Size, version: ref.Version}, nil
}
func validSource(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && validRelative(name, false) && !strings.Contains(name, "/")
}
func validRelative(value string, empty bool) bool {
	if value == "" {
		return empty
	}
	if strings.ContainsAny(value, "\\:") || strings.HasPrefix(value, "/") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func validType(value string) bool {
	return value == "" || (validRelative(value, false) && !strings.Contains(value, "/"))
}

// Context returns the processing Context supplied to a custom file handler.
func (f *File) Context() context.Context {
	if f.ctx == nil {
		return context.Background()
	}
	return f.ctx
}
