package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func TestUnsupportedOptionsPreserveObject(t *testing.T) {
	resetRuntime(t)
	dubi.Register("default", Config{Settings: Map{"base_url": "https://files.example.test"}})
	startRuntime(t)
	file, err := UploadReader(strings.NewReader("original"), UploadOption{Key: "same"})
	if err != nil {
		t.Fatal(err)
	}
	for _, opt := range []UploadOption{
		{Metadata: Map{"owner": "user"}}, {Tags: Map{"public": true}}, {Expires: time.Now()},
	} {
		opt.Key = "same"
		reader := strings.NewReader("replacement")
		if _, err := UploadReader(reader, opt); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unsupported upload: %v", err)
		}
		if reader.Len() != len("replacement") || read(t, file.Code()) != "original" {
			t.Fatal("unsupported options consumed input or changed object")
		}
	}
	for _, opt := range []BrowseOption{{Expires: time.Minute}, {Headers: Map{"X-Owner": "user"}}} {
		if _, err := Browse(file.Code(), opt); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("silently ignored URL option: %v", err)
		}
	}
	if _, err := Browse(file.Code(), BrowseOption{Expires: -1}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("negative expiry: %v", err)
	}
	if url, err := Browse(file.Code(), BrowseOption{Params: Map{"download": "yes"}}); err != nil || !strings.HasSuffix(url, "?download=yes") {
		t.Fatalf("ordinary URL params: %q %v", url, err)
	}
}

func TestHashVersionAndLegacyReferences(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	file, err := UploadReader(strings.NewReader("content"), UploadOption{Key: "same"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := Stat(file.Code())
	if err != nil || file.Hash() != "040f06fd774092478d450774f5ba30c5da78acc8" || file.Version() == "" || file.Version() != info.Version {
		t.Fatalf("hash/version: %+v %+v %v", file, info, err)
	}
	decoded, err := Decode(file.Code())
	if err != nil || decoded.Hash() != "" || decoded.Version() != "" || decoded.File() != file.File() || decoded.Size() != file.Size() {
		t.Fatalf("compact reference: %+v %v", decoded, err)
	}
	old, err := json.Marshal(fileReference{Base: file.Base(), Key: file.Key(), Size: file.Size(), Version: digest("content")})
	if err != nil {
		t.Fatal(err)
	}
	legacy := "v1." + base64.RawURLEncoding.EncodeToString(old)
	decoded, err = Decode(legacy)
	if err != nil || decoded.Hash() != digest("content") || decoded.Version() != "" || read(t, legacy) != "content" {
		t.Fatalf("legacy hash interpreted as backend version: %+v %v", decoded, err)
	}
	previous, err := json.Marshal(fileReference{Base: file.Base(), Key: file.Key(), Size: file.Size(), Hash: digest("content"), Version: file.Version()})
	if err != nil {
		t.Fatal(err)
	}
	v2 := "v2." + base64.RawURLEncoding.EncodeToString(previous)
	decoded, err = Decode(v2)
	if err != nil || decoded.Hash() != digest("content") || decoded.Version() != file.Version() || decoded.Code() != file.Code() || read(t, v2) != "content" {
		t.Fatalf("v2 compatibility: %+v %v", decoded, err)
	}
	if _, err := UploadReader(strings.NewReader("updated"), UploadOption{Key: "same"}); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{legacy, v2, file.Code()} {
		name, err := Download(code)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(name)
		if err != nil || string(data) != "updated" {
			t.Fatalf("reference pinned old content: %q %v", data, err)
		}
	}
}

func TestCompactCodeFormat(t *testing.T) {
	for _, test := range []struct {
		file File
		raw  string
	}{
		{File{base: "default", prefix: "aa/bb", key: "key", typee: "txt", size: 123}, "\taa/bb\tkey\ttxt\t123"},
		{File{base: "archive", key: "文件", size: 0}, "archive\t\t文件\t\t0"},
	} {
		file := test.file
		code := file.Code()
		if code != base64.RawURLEncoding.EncodeToString([]byte(test.raw)) {
			t.Fatalf("unexpected compact encoding: %q", code)
		}
		file.hash, file.version = "040f06fd774092478d450774f5ba30c5da78acc8", "backend-revision"
		if file.Code() != code {
			t.Fatal("hash/version must not be included in Code")
		}
		decoded, err := Decode(code)
		if err != nil || decoded.Base() != file.Base() || decoded.File() != file.File() || decoded.Size() != file.Size() || decoded.Code() != code {
			t.Fatalf("compact round trip: %+v %v", decoded, err)
		}
	}
}

func TestDeclaredUploadSize(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	file, err := UploadReader(strings.NewReader("original"), UploadOption{Key: "same"})
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{0, 2, 10} {
		if _, err := UploadReader(strings.NewReader("new"), UploadOption{Key: "same", Size: &size}); !errors.Is(err, ErrSizeMismatch) {
			t.Fatalf("declared size %d: %v", size, err)
		}
		if read(t, file.Code()) != "original" {
			t.Fatal("size mismatch overwrote previous object")
		}
	}
	for _, mode := range []KeyMode{HashKey, RandomKey} {
		size := int64(4)
		if _, err := UploadReader(strings.NewReader("new"), UploadOption{KeyMode: mode, Size: &size}); !errors.Is(err, ErrSizeMismatch) {
			t.Fatalf("%s size mismatch: %v", mode, err)
		}
		size = 0
		file, err := UploadReader(strings.NewReader(""), UploadOption{KeyMode: mode, Size: &size})
		if err != nil || file.Size() != 0 || read(t, file.Code()) != "" {
			t.Fatalf("%s empty upload: %+v %v", mode, file, err)
		}
	}
	negative := int64(-1)
	if _, err := UploadReader(strings.NewReader("new"), UploadOption{Size: &negative}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("negative size: %v", err)
	}
}

// Change the startup configuration without mutating a running module.
func setStorageSection(t *testing.T, section string, options Map) {
	t.Helper()
	name := os.Getenv("DUBI_CONFIG")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	values := config["storage"].(map[string]any)[section].(map[string]any)
	for key, value := range options {
		values[key] = value
	}
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStreamingWithoutWritableCacheOrTemp(t *testing.T) {
	resetRuntime(t)
	blocked := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	options := Map{}
	for _, key := range []string{"download", "thumbnail", "preview"} {
		options[key] = filepath.Join(blocked, key)
	}
	setStorageSection(t, "options", options)
	setStorageSection(t, "settings", Map{"temp": filepath.Join(blocked, "temp"), "key_mode": "random"})
	driver := &testDriver{}
	RegisterDriver("test-lazy", driver)
	dubi.Register("default", Config{Type: "test-lazy"})
	startRuntime(t)
	var previous string
	for range 2 {
		file, err := UploadReader(strings.NewReader("content"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(file.Key(), "remote-") || file.Key() == previous || file.Hash() != "" || file.Prefix() != "" || read(t, file.Code()) != "content" {
			t.Fatalf("driver-owned naming: %+v", file)
		}
		previous = file.Key()
		if _, err := Download(file.Code()); err == nil {
			t.Fatal("cache operation should report unusable cache path")
		}
		if err := Remove(file.Code()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := UploadReader(strings.NewReader("hash"), UploadOption{KeyMode: HashKey}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("external driver should reject local naming policy: %v", err)
	}
	file, err := UploadReader(strings.NewReader("explicit"), UploadOption{Key: "chosen"})
	if err != nil || file.Key() != "chosen" || read(t, file.Code()) != "explicit" {
		t.Fatalf("explicit key after cache failure: %+v %v", file, err)
	}
	if _, err := UploadReader(strings.NewReader("unsupported"), UploadOption{Tags: Map{"x": "y"}}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("driver without validator accepted tags: %v", err)
	}
}

func TestLazyDirectoriesAndConcurrentFirstDownload(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	assertAbsent := func(names ...string) {
		t.Helper()
		for _, name := range names {
			if _, err := os.Stat(name); !os.IsNotExist(err) {
				t.Fatalf("directory created prematurely: %s (%v)", name, err)
			}
		}
	}
	opt := module.options
	temp := module.instances["default"].conn.(*fileConnection).temp
	assertAbsent(temp, opt.Download, opt.Thumbnail, opt.Preview)
	file, err := UploadReader(strings.NewReader("content"), UploadOption{KeyMode: RandomKey})
	if err != nil {
		t.Fatal(err)
	}
	assertAbsent(temp, opt.Download, opt.Thumbnail, opt.Preview)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			name, err := Download(file.Code())
			if err != nil {
				t.Error(err)
				return
			}
			data, err := os.ReadFile(name)
			if err != nil || string(data) != "content" {
				t.Errorf("download: %q %v", data, err)
			}
		})
	}
	wg.Wait()
	assertAbsent(temp, opt.Thumbnail, opt.Preview)
	if _, err := UploadReader(strings.NewReader("hashed")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging files not cleaned: %v %v", entries, err)
	}
}

type metadataDriver struct{ conn *metadataConnection }
type metadataConnection struct {
	*testConnection
	metadata Map
}

func (d *metadataDriver) Connect(inst *Instance) (Connection, error) {
	d.conn = &metadataConnection{testConnection: &testConnection{instance: inst, driver: &testDriver{}, objects: map[string][]byte{}}}
	return d.conn, nil
}
func (c *metadataConnection) ValidateUpload(opt UploadOption) error {
	opt.Metadata = nil // This test backend supports Metadata, but not Tags/Expires.
	return unsupportedUploadOptions(opt)
}
func (c *metadataConnection) Upload(reader io.Reader, opt UploadOption) (*File, error) {
	file, err := c.testConnection.Upload(reader, opt)
	if err != nil {
		return nil, err
	}
	c.metadata = opt.Metadata
	return c.instance.NewFile(file.Prefix(), file.Key(), file.Type(), file.Size(), ObjectInfo{Hash: "driver-checksum", Version: "backend-revision"}), nil
}

func TestDriverMetadataAndOpaqueVersion(t *testing.T) {
	resetRuntime(t)
	driver := &metadataDriver{}
	RegisterDriver("test-metadata", driver)
	dubi.Register("default", Config{Type: "test-metadata"})
	startRuntime(t)
	metadata := Map{"owner": "user"}
	file, err := UploadReader(strings.NewReader("content"), UploadOption{Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	metadata["owner"] = "changed"
	if driver.conn.metadata["owner"] != "user" || file.Version() != "backend-revision" || file.Hash() != "driver-checksum" {
		t.Fatalf("metadata/version not preserved: %+v %+v", file, driver.conn.metadata)
	}
	if _, err := UploadReader(strings.NewReader("content"), UploadOption{Tags: Map{"x": "y"}}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("validator ignored unsupported tags: %v", err)
	}
}
