package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func multipartUpload(t *testing.T, name, contentType, content string, memory int64) (*multipart.Form, *multipart.FileHeader) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="` + name + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&body, writer.Boundary()).ReadForm(memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form, form.File["file"][0]
}

func TestMultipartUploadMemoryAndDisk(t *testing.T) {
	for _, disk := range []bool{false, true} {
		name := "memory"
		memory := int64(1 << 20)
		if disk {
			name, memory = "disk", 0
		}
		t.Run(name, func(t *testing.T) {
			resetRuntime(t)
			t.Setenv("TMPDIR", t.TempDir())
			startRuntime(t)
			form, header := multipartUpload(t, "hello.txt", "application/octet-stream", "uploaded content", memory)
			body, err := header.Open()
			if err != nil {
				t.Fatal(err)
			}
			_, isDisk := body.(*os.File)
			_ = body.Close()
			if isDisk != disk {
				t.Fatal("multipart fixture uses unexpected storage")
			}
			file, err := Upload(header)
			if err != nil {
				t.Fatal(err)
			}
			if file.Type() != "txt" || file.Size() != header.Size || read(t, file.Code()) != "uploaded content" {
				t.Fatalf("multipart result: %+v", file)
			}
			// Storage owns its opened stream, not the request's multipart files.
			again, err := Upload(header)
			if err != nil || again.Code() != file.Code() {
				t.Fatalf("request upload no longer reusable: %+v %v", again, err)
			}
			if err := form.RemoveAll(); err != nil {
				t.Fatal(err)
			}
			if read(t, file.Code()) != "uploaded content" {
				t.Fatal("stored object depends on request's temporary file")
			}
		})
	}
}

func TestMultipartUploadOptionsAndErrors(t *testing.T) {
	resetRuntime(t)
	dubi.Register("archive", Config{Weight: -1, Prefix: "archive", Settings: Map{"root": filepath.Join(t.TempDir(), "objects")}})
	startRuntime(t)
	_, header := multipartUpload(t, "original.txt", "text/plain", "content", 1024)
	for _, opt := range []UploadOption{
		{Key: "name", Name: "changed.png"},
		{Key: "type", Type: "png"},
		{Key: "mime", Mimetype: "image/png"},
	} {
		file, err := UploadTo("archive", header, opt)
		if err != nil || file.Base() != "archive" || file.Prefix() != "archive" || file.Key() != opt.Key || file.Type() != "png" {
			t.Fatalf("multipart options: %+v %v", file, err)
		}
	}
	_, noExt := multipartUpload(t, "photo", "image/jpeg", "image bytes", 1024)
	file, err := Upload(noExt)
	if err != nil || file.Type() != "jpg" {
		t.Fatalf("MIME fallback: %+v %v", file, err)
	}
	_, empty := multipartUpload(t, "empty.txt", "text/plain", "", 1024)
	file, err = Upload(empty)
	if err != nil || file.Size() != 0 || read(t, file.Code()) != "" {
		t.Fatalf("empty multipart file: %+v %v", file, err)
	}
	var missing *multipart.FileHeader
	if _, err := Upload(missing); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("nil header: %v", err)
	}
	wrong := int64(2)
	if _, err := Upload(header, UploadOption{Size: &wrong}); !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("size mismatch: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Upload(header, UploadOption{Context: ctx}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled multipart upload: %v", err)
	}
	form, removed := multipartUpload(t, "removed.txt", "text/plain", "content", 0)
	if err := form.RemoveAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := Upload(removed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open error not propagated: %v", err)
	}
}
