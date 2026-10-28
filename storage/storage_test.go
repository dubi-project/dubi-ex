package storage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func resetRuntime(t *testing.T) {
	t.Helper()
	dubi.Reset()
	t.Cleanup(dubi.Reset)
	root := t.TempDir()
	config := filepath.Join(root, "config.json")
	text := fmt.Sprintf(`{"storage":{"settings":{"root":%q},"options":{"download":%q,"thumbnail":%q,"preview":%q,"temp":%q}}}`, filepath.Join(root, "objects"), filepath.Join(root, "download"), filepath.Join(root, "thumbnail"), filepath.Join(root, "preview"), filepath.Join(root, "temp"))
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DUBI_CONFIG", config)
}
func startRuntime(t *testing.T, profile ...string) {
	t.Helper()
	if err := dubi.DefaultRuntime().Run(profile...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dubi.End(); err != nil {
			t.Error(err)
		}
		dubi.Reset()
	})
}
func read(t *testing.T, code string, opts ...FetchOption) string {
	t.Helper()
	body, err := Fetch(code, opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFileRoundTripPrefixAndSelfUpload(t *testing.T) {
	resetRuntime(t)
	dubi.Register("default", &Config{Prefix: "tenant/"})
	startRuntime(t)
	if !Ready() {
		t.Fatal("not ready")
	}
	source := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(source, []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := Upload(Map{"file": source})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(file.Prefix(), "tenant/") || file.Size() != 6 || file.Version() == "" {
		t.Fatalf("file: %+v", file)
	}
	decoded, err := Decode(file.Code())
	if err != nil || decoded.File() != file.File() || decoded.Size() != 6 {
		t.Fatalf("decode: %+v %v", decoded, err)
	}
	if got := read(t, file.Code()); got != "abcdef" {
		t.Fatalf("content: %s", got)
	}
	local, err := Local(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	again, err := Upload(local)
	if err != nil {
		t.Fatal(err)
	}
	if again.Code() != file.Code() || read(t, again.Code()) != "abcdef" {
		t.Fatal("self-upload damaged object")
	}
	manual, err := UploadReader(strings.NewReader("manual"), UploadOption{Key: "named", Prefix: "business", Name: "name.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Prefix() != "tenant/business" {
		t.Fatalf("explicit key prefix: %s", manual.Prefix())
	}
	target := filepath.Join(t.TempDir(), "export.txt")
	got, err := Download(file.Code(), DownloadOption{Target: target})
	if err != nil || got != target {
		t.Fatalf("target: %s %v", got, err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "abcdef" {
		t.Fatal("download content")
	}
	cache, err := Download(file.Code())
	if err != nil || cache == local {
		t.Fatalf("download must return a managed copy: %s %v", cache, err)
	}
	if _, err := Browse(file.Code()); !errors.Is(err, ErrBrowseUnsupported) {
		t.Fatalf("local path exposed as URL: %v", err)
	}
}

func TestRangeZeroAndRandomAccess(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	file, err := UploadReader(strings.NewReader("abcdef"), UploadOption{Name: "test.txt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		rng  *Range
		want string
	}{{nil, "abcdef"}, {&Range{0, 0}, "a"}, {&Range{2, 3}, "cd"}, {&Range{3, -1}, "def"}, {&Range{4, 99}, "ef"}} {
		if got := read(t, file.Code(), FetchOption{Range: test.rng}); got != test.want {
			t.Fatalf("range %+v: %q", test.rng, got)
		}
	}
	for _, rng := range []*Range{{-1, 0}, {4, 2}, {10, -1}, {0, -2}} {
		if body, err := Fetch(file.Code(), FetchOption{Range: rng}); !errors.Is(err, ErrInvalidRange) {
			if body != nil {
				body.Close()
			}
			t.Fatalf("invalid range %+v: %v", rng, err)
		}
	}
	body, err := Fetch(file.Code(), FetchOption{Range: &Range{2, 4}})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	access, ok := body.(RandomAccessStream)
	if !ok {
		t.Fatal("lost optional random access")
	}
	data := make([]byte, 1)
	if _, err := access.ReadAt(data, 1); err != nil || string(data) != "d" {
		t.Fatalf("ReadAt %q %v", data, err)
	}
}

type failingReader struct{ once bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.once {
		r.once = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("source failed")
}
func TestInterruptedWritePreservesObjectAndExports(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	opt := UploadOption{Key: "same", Name: "file.txt"}
	file, err := UploadReader(strings.NewReader("original"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UploadReader(&failingReader{}, opt); err == nil {
		t.Fatal("accepted partial upload")
	}
	if got := read(t, file.Code()); got != "original" {
		t.Fatalf("old object lost: %q", got)
	}
	root := module.instances["default"].conn.(*fileConnection).directory
	if err := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".upload-") {
			t.Errorf("temporary file leaked: %s", name)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := UploadReader(bytes.NewReader([]byte("data")), UploadOption{Context: ctx, Key: "same"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled upload: %v", err)
	}
}

func TestOverwriteAndRemoveInvalidateDownloadCache(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	opt := UploadOption{Key: "mutable", Name: "file.txt"}
	one, err := UploadReader(strings.NewReader("first"), opt)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := Download(one.Code())
	if err != nil {
		t.Fatal(err)
	}
	two, err := UploadReader(strings.NewReader("second"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if one.Code() == two.Code() {
		t.Fatal("version missing from reference")
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("old cache survived: %v", err)
	}
	current, err := Download(one.Code())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(current)
	if string(data) != "second" {
		t.Fatalf("old reference returned stale data: %q", data)
	}
	if err := Remove(two.Code()); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(one.Code()); !os.IsNotExist(err) {
		t.Fatalf("removed object returned cached bytes: %v", err)
	}
	if err := Remove(two.Code()); err != nil {
		t.Fatal(err)
	}
}

func TestPathsCodeAndSymlinkConfinement(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	for _, opt := range []UploadOption{{Key: "../escape"}, {Key: "/absolute"}, {Key: "valid", Prefix: "../parent"}, {Key: "valid", Type: "../txt"}, {Key: "bad\tkey"}} {
		if _, err := UploadReader(strings.NewReader("x"), opt); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("accepted path %+v: %v", opt, err)
		}
	}
	for _, code := range []string{"bad", "v1.e30", base64.RawURLEncoding.EncodeToString([]byte("\t../escape\tkey\ttxt\t1")), base64.RawURLEncoding.EncodeToString([]byte("\tvalid\tkey\ttxt\t-1"))} {
		if _, err := Decode(code); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("accepted code %q: %v", code, err)
		}
	}
	legacy := base64.RawURLEncoding.EncodeToString([]byte("\taa/bb\tkey\ttxt\t1"))
	if file, err := Decode(legacy); err != nil || file.Base() != "default" {
		t.Fatalf("legacy decode: %+v %v", file, err)
	}
	outside := t.TempDir()
	root := module.instances["default"].conn.(*fileConnection).directory
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if _, err := UploadReader(strings.NewReader("x"), UploadOption{Key: "link/escape"}); err == nil {
		t.Fatal("symlink escaped root")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
		t.Fatal("wrote outside root")
	}
}

func TestConcurrentUploadAndFetch(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	opt := UploadOption{Key: "shared", Name: "file.txt"}
	file, err := UploadReader(strings.NewReader(strings.Repeat("a", 4096)), opt)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if i%2 == 0 {
					if _, err := UploadReader(strings.NewReader(strings.Repeat("b", 4096)), opt); err != nil {
						t.Error(err)
					}
				} else {
					body, err := Fetch(file.Code())
					if err != nil {
						t.Error(err)
						return
					}
					data, err := io.ReadAll(body)
					body.Close()
					if err != nil || len(data) != 4096 {
						t.Errorf("partial fetch: %d %v", len(data), err)
					}
				}
			}
		}(i)
	}
	wg.Wait()
}
