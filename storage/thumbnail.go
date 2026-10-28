package storage

import (
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
)

func (m *Module) generateImage(ctx context.Context, inst *Instance, file *File, width, height, position int64, kind string) (string, error) {
	if width < 0 || height < 0 || position < 0 || position > 8 {
		return "", ErrInvalidOption
	}
	unlock := m.objects.lock(objectID(file), false)
	defer unlock()
	version, reuse, err := m.objectVersion(ctx, inst, file)
	if err != nil {
		return "", err
	}
	typee, format := thumbnailFormat(file.Type())
	target := filepath.Join(objectID(file), version, fmt.Sprintf("%d-%d-%d.%s", width, height, position, typee))
	root := m.cache[kind]
	variantUnlock := m.objects.lock(kind+":"+target, true)
	defer variantUnlock()
	if reuse && m.fresh(root, target) {
		return filepath.Join(root.Name(), target), nil
	}
	source, err := m.downloadLocked(ctx, inst, file)
	if err != nil {
		return "", err
	}
	config, err := ImageConfig(source)
	if err != nil {
		return "", err
	}
	if exceedsPixels(int64(config.Width), int64(config.Height), m.options.MaxImagePixels) {
		return "", ErrImageTooLarge
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	img, err := imaging.Open(source, imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}
	w, h, err := thumbnailSize(img.Bounds(), width, height, m.options.MaxThumbnailPixels)
	if err != nil {
		return "", err
	}
	anchors := []imaging.Anchor{imaging.Center, imaging.TopLeft, imaging.Top, imaging.TopRight, imaging.Left, imaging.Right, imaging.BottomLeft, imaging.Bottom, imaging.BottomRight}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	output := imaging.Fill(img, w, h, anchors[position], imaging.Lanczos)
	if err = atomicRootGenerate(ctx, root, target, func(writer io.Writer) error { return imaging.Encode(writer, output, format) }); err != nil {
		return "", err
	}
	return filepath.Join(root.Name(), target), nil
}
func thumbnailFormat(typee string) (string, imaging.Format) {
	switch typee {
	case "png":
		return "png", imaging.PNG
	case "gif":
		return "gif", imaging.GIF
	case "bmp":
		return "bmp", imaging.BMP
	case "tif", "tiff":
		return "tiff", imaging.TIFF
	default:
		return "jpg", imaging.JPEG
	}
}
func exceedsPixels(width, height, limit int64) bool {
	return width <= 0 || height <= 0 || width > limit/height
}
func thumbnailSize(bounds image.Rectangle, width, height, limit int64) (int, int, error) {
	ow, oh := int64(bounds.Dx()), int64(bounds.Dy())
	if ow <= 0 || oh <= 0 {
		return 0, 0, ErrInvalidOption
	}
	if width == 0 && height == 0 {
		width, height = ow, oh
	} else if width == 0 {
		if height > limit {
			return 0, 0, ErrImageTooLarge
		}
		width = max(1, int64(float64(height)*float64(ow)/float64(oh)))
	} else if height == 0 {
		if width > limit {
			return 0, 0, ErrImageTooLarge
		}
		height = max(1, int64(float64(width)*float64(oh)/float64(ow)))
	}
	if exceedsPixels(width, height, limit) || int64(int(width)) != width || int64(int(height)) != height {
		return 0, 0, ErrImageTooLarge
	}
	return int(width), int(height), nil
}
func DecodeImageConfig(reader io.Reader) (image.Config, error) {
	config, _, err := image.DecodeConfig(reader)
	return config, err
}
func ImageConfig(name string) (image.Config, error) {
	file, err := os.Open(name)
	if err != nil {
		return image.Config{}, err
	}
	defer file.Close()
	return DecodeImageConfig(file)
}
