package storage

import (
	"context"
	"errors"
	"mime/multipart"
	"path/filepath"
	"strings"
	"testing"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func TestMultipartBatchUploads(t *testing.T) {
	resetRuntime(t)
	dubi.Register("archive", Config{Weight: -1, Prefix: "archive", Settings: Map{
		"root": filepath.Join(t.TempDir(), "objects"), "key_mode": "random",
	}})
	startRuntime(t)
	_, first := multipartUpload(t, "one.txt", "text/plain", "first", 1024)
	_, second := multipartUpload(t, "two.json", "application/json", "{}", 0)
	inputs := []any{[]*multipart.FileHeader{first, second}, []multipart.FileHeader{*first, *second}}
	for _, input := range inputs {
		files, err := UploadsTo("archive", input, UploadOption{Prefix: "batch"})
		if err != nil || len(files) != 2 {
			t.Fatalf("batch: %+v %v", files, err)
		}
		if files[0].Type() != "txt" || files[1].Type() != "json" || read(t, files[0].Code()) != "first" || read(t, files[1].Code()) != "{}" {
			t.Fatal("batch lost order, contents, or individual metadata")
		}
		for _, file := range files {
			if file.Base() != "archive" || !strings.HasPrefix(file.Prefix(), "archive/batch/") {
				t.Fatalf("connection or options not applied: %+v", file)
			}
		}
	}
	files, err := Uploads([]*multipart.FileHeader{first, second})
	if err != nil || len(files) != 2 || files[0].Base() != "default" || files[1].Base() != "default" {
		t.Fatalf("automatic connection selection: %+v %v", files, err)
	}
}

func TestMultipartBatchPartialFailure(t *testing.T) {
	resetRuntime(t)
	driver := &testDriver{}
	RegisterDriver("batch-test", driver)
	dubi.Register("default", Config{Type: "batch-test"})
	startRuntime(t)
	_, first := multipartUpload(t, "one.txt", "text/plain", "first", 1024)
	_, last := multipartUpload(t, "last.txt", "text/plain", "last", 1024)
	files, err := Uploads([]*multipart.FileHeader{first, nil, last})
	if !errors.Is(err, ErrInvalidOption) || !strings.Contains(err.Error(), "index 1") || len(files) != 1 {
		t.Fatalf("partial failure: %+v %v", files, err)
	}
	if read(t, files[0].Code()) != "first" || len(driver.connections[0].objects) != 1 {
		t.Fatal("successful file rolled back or upload continued after failure")
	}
	if _, err := Uploads([]*multipart.FileHeader{first, last}, UploadOption{Key: "same"}); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("shared batch key accepted: %v", err)
	}
	if len(driver.connections[0].objects) != 1 {
		t.Fatal("invalid batch wrote objects")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	files, err = Uploads([]*multipart.FileHeader{first, last}, UploadOption{Context: ctx})
	if !errors.Is(err, context.Canceled) || len(files) != 0 || len(driver.connections[0].objects) != 1 {
		t.Fatalf("canceled batch: %+v %v", files, err)
	}
}

func TestMultipartEmptyBatch(t *testing.T) {
	for _, input := range []any{[]*multipart.FileHeader(nil), []multipart.FileHeader{}} {
		files, err := Uploads(input)
		if err != nil || len(files) != 0 {
			t.Fatalf("empty batch: %+v %v", files, err)
		}
	}
	if _, err := Uploads("not a file list"); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("invalid batch type: %v", err)
	}
}
