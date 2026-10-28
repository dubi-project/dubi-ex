package storage

import (
	"context"
	"strings"
)

type ThumbnailFunc func(*File, int64, int64, int64) (string, error)
type Thumbnailer struct {
	Name, Desc string
	Alias      []string
	Action     ThumbnailFunc
	builtin    bool
}
type PreviewerFunc func(*File, int64, int64, int64) (string, error)
type Previewer struct {
	Name, Desc string
	Alias      []string
	Action     PreviewerFunc
	builtin    bool
}

func (Thumbnailer) Scope() string { return "storage:thumbnailer" }
func (Previewer) Scope() string   { return "storage:previewer" }
func (m *Module) resetHandlers() {
	m.thumbnailers = map[string]Thumbnailer{}
	m.previewers = map[string]Previewer{}
	for _, key := range []string{"image", "jpg", "jpeg", "png", "gif", "bmp", "webp", "tif", "tiff"} {
		m.thumbnailers[key] = Thumbnailer{Name: "Image", builtin: true}
		m.previewers[key] = Previewer{Name: "Image", builtin: true}
	}
}
func aliases(name string, values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range append([]string{name}, values...) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	return out
}
func (m *Module) RegisterThumbnailer(name string, cfg Thumbnailer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened || m.closing {
		panic("storage: register processor after open")
	}
	cfg.Alias = append([]string(nil), cfg.Alias...)
	for _, key := range aliases(name, cfg.Alias) {
		m.thumbnailers[key] = cfg
	}
}
func (m *Module) RegisterPreviewer(name string, cfg Previewer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened || m.closing {
		panic("storage: register processor after open")
	}
	cfg.Alias = append([]string(nil), cfg.Alias...)
	for _, key := range aliases(name, cfg.Alias) {
		m.previewers[key] = cfg
	}
}
func Thumbnail(code string, width, height, position int64) (string, error) {
	return ThumbnailContext(context.Background(), code, width, height, position)
}
func ThumbnailContext(parent context.Context, code string, width, height, position int64) (string, error) {
	ctx, release, err := module.begin(parent)
	if err != nil {
		return "", err
	}
	defer release()
	inst, file, err := module.resolve(code)
	if err != nil {
		return "", err
	}
	cfg, ok := module.thumbnailers[file.Type()]
	if !ok {
		return "", ErrInvalidHandler
	}
	if cfg.builtin {
		return module.generateImage(ctx, inst, file, width, height, position, "thumbnail")
	}
	if cfg.Action == nil {
		return "", ErrInvalidHandler
	}
	file.ctx = ctx
	return cfg.Action(file, width, height, position)
}
func Preview(code string, width, height int64) (string, error) {
	return PreviewContext(context.Background(), code, width, height)
}
func PreviewContext(parent context.Context, code string, width, height int64) (string, error) {
	ctx, release, err := module.begin(parent)
	if err != nil {
		return "", err
	}
	defer release()
	inst, file, err := module.resolve(code)
	if err != nil {
		return "", err
	}
	cfg, ok := module.previewers[file.Type()]
	if !ok {
		return "", ErrInvalidHandler
	}
	if cfg.builtin {
		return module.generateImage(ctx, inst, file, width, height, 0, "preview")
	}
	if cfg.Action == nil {
		return "", ErrInvalidHandler
	}
	file.ctx = ctx
	return cfg.Action(file, width, height, 0)
}
