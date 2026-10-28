package storage

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func pngData(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, c)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
func TestThumbnailPreviewAndMutationInvalidation(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	file, err := UploadReader(bytes.NewReader(pngData(t, color.RGBA{R: 255, A: 255})), UploadOption{Key: "image", Name: "image.png"})
	if err != nil {
		t.Fatal(err)
	}
	thumb, err := Thumbnail(file.Code(), 20, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ImageConfig(thumb)
	if err != nil || cfg.Width != 20 || cfg.Height != 10 {
		t.Fatalf("thumbnail: %+v %v", cfg, err)
	}
	again, err := Thumbnail(file.Code(), 20, 0, 0)
	if err != nil || again != thumb {
		t.Fatalf("cache: %s %v", again, err)
	}
	preview, err := Preview(file.Code(), 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if preview == thumb {
		t.Fatal("preview shares thumbnail destination")
	}
	updated, err := UploadReader(bytes.NewReader(pngData(t, color.RGBA{B: 255, A: 255})), UploadOption{Key: "image", Name: "image.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(thumb); !os.IsNotExist(err) {
		t.Fatal("old thumbnail survived overwrite")
	}
	if _, err := os.Stat(preview); !os.IsNotExist(err) {
		t.Fatal("old preview survived overwrite")
	}
	newThumb, err := Thumbnail(file.Code(), 20, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := os.Open(newThumb)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(opened)
	opened.Close()
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := img.At(0, 0).RGBA()
	if b <= r {
		t.Fatal("old reference returned stale thumbnail")
	}
	if err := Remove(updated.Code()); err != nil {
		t.Fatal(err)
	}
	if _, err := Thumbnail(file.Code(), 20, 0, 0); !os.IsNotExist(err) {
		t.Fatalf("removed file returned thumbnail: %v", err)
	}
}
func TestCustomProcessorsAndLimits(t *testing.T) {
	resetRuntime(t)
	seen := false
	dubi.Register("text", &Thumbnailer{Name: "Text", Desc: "Custom processor", Alias: []string{" TXT "}, Action: func(file *File, w, h, p int64) (string, error) {
		if file.Context().Err() != nil {
			t.Error("lost handler context")
		}
		seen = true
		return "custom", nil
	}})
	startRuntime(t)
	file, err := UploadReader(strings.NewReader("text"), UploadOption{Name: "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if target, err := Thumbnail(file.Code(), 10, 10, 0); err != nil || target != "custom" || !seen {
		t.Fatalf("custom handler: %s %v", target, err)
	}
	imageFile, err := UploadReader(bytes.NewReader(pngData(t, color.Black)), UploadOption{Name: "image.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Thumbnail(imageFile.Code(), 1<<40, 1<<40, 0); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("oversized output: %v", err)
	}
	if _, err := Thumbnail(imageFile.Code(), 20, 20, 9); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("invalid position: %v", err)
	}
	module.options.MaxImagePixels = 10
	if _, err := Thumbnail(imageFile.Code(), 30, 20, 0); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("oversized source: %v", err)
	}
}
func TestFileJSONIsUseful(t *testing.T) {
	file := (&Instance{Name: "default"}).NewFile("prefix", "key", "txt", 7)
	data, err := dubi.Marshal("json", Map{"file": file})
	if err != nil || !bytes.Contains(data, []byte(`"code"`)) || !bytes.Contains(data, []byte(`"name":"key.txt"`)) {
		t.Fatalf("file JSON: %s %v", data, err)
	}
}
