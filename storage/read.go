package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
)

func Fetch(code string, opts ...FetchOption) (Stream, error) {
	opt := FetchOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	return module.fetch(code, opt)
}
func (m *Module) fetch(code string, opt FetchOption) (Stream, error) {
	ctx, release, err := m.begin(opt.Context)
	if err != nil {
		return nil, err
	}
	inst, file, err := m.resolve(code)
	if err != nil {
		release()
		return nil, err
	}
	opt.Context = ctx
	if opt.Range != nil {
		value := *opt.Range
		opt.Range = &value
		if value.Start < 0 || value.End < -1 || (value.End >= 0 && value.End < value.Start) {
			release()
			return nil, ErrInvalidRange
		}
	}
	unlock := m.objects.lock(objectID(file), false)
	body, err := inst.conn.Fetch(file, opt)
	unlock()
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		release()
		return nil, err
	}
	if body == nil {
		release()
		return nil, ErrInvalidOption
	}
	return manage(ctx, body, release), nil
}
func Download(code string, opts ...DownloadOption) (string, error) {
	opt := DownloadOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	return module.download(code, opt)
}
func (m *Module) download(code string, opt DownloadOption) (string, error) {
	ctx, release, err := m.begin(opt.Context)
	if err != nil {
		return "", err
	}
	defer release()
	inst, file, err := m.resolve(code)
	if err != nil {
		return "", err
	}
	unlock := m.objects.lock(objectID(file), false)
	defer unlock()
	if opt.Target == "" {
		return m.downloadLocked(ctx, inst, file)
	}
	body, err := inst.conn.Fetch(file, FetchOption{Context: ctx})
	if err != nil {
		return "", err
	}
	if body == nil {
		return "", ErrInvalidOption
	}
	body = manage(ctx, body, func() {})
	defer body.Close()
	if _, err = atomicPathWrite(ctx, opt.Target, body); err != nil {
		return "", err
	}
	return opt.Target, nil
}

func (m *Module) objectVersion(ctx context.Context, inst *Instance, file *File) (string, bool, error) {
	if statter, ok := inst.conn.(Statter); ok {
		info, err := statter.Stat(file, StatOption{Context: ctx})
		if err != nil {
			return "", false, err
		}
		if info.Version != "" {
			return digest(info.Version), true, nil
		}
	}
	// Without backend freshness information, persistent caches are never reused.
	return "unversioned", false, nil
}
func (m *Module) downloadLocked(ctx context.Context, inst *Instance, file *File) (string, error) {
	version, reuse, err := m.objectVersion(ctx, inst, file)
	if err != nil {
		return "", err
	}
	root, err := m.cacheRoot("download")
	if err != nil {
		return "", err
	}
	target := filepath.Join(objectID(file), version, "object"+typeSuffix(file.typee))
	unlock := m.objects.lock("download:"+target, true)
	defer unlock()
	if reuse && m.fresh(root, target) {
		return filepath.Join(root.Name(), target), nil
	}
	body, err := inst.conn.Fetch(file, FetchOption{Context: ctx})
	if err != nil {
		return "", err
	}
	if body == nil {
		return "", ErrInvalidOption
	}
	body = manage(ctx, body, func() {})
	defer body.Close()
	if _, err = atomicRootWrite(ctx, root, target, body); err != nil {
		return "", err
	}
	return filepath.Join(root.Name(), target), nil
}
func (m *Module) fresh(root *os.Root, target string) bool {
	info, err := root.Stat(target)
	return err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) < m.options.CacheTTL
}
func typeSuffix(typee string) string {
	if typee == "" {
		return ""
	}
	return "." + typee
}
func (m *Module) invalidate(file *File) error {
	// Only opened caches need eager invalidation. Other caches verify the
	// backend version on first use (or never reuse data without a version).
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	var errs []error
	for _, root := range m.cache {
		errs = append(errs, root.RemoveAll(objectID(file)))
	}
	return errors.Join(errs...)
}

func Remove(code string, opts ...RemoveOption) error {
	opt := RemoveOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	ctx, release, err := module.begin(opt.Context)
	if err != nil {
		return err
	}
	defer release()
	inst, file, err := module.resolve(code)
	if err != nil {
		return err
	}
	unlock := module.objects.lock(objectID(file), true)
	defer unlock()
	opt.Context = ctx
	if err = inst.conn.Remove(file, opt); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return module.invalidate(file)
}
func Local(code string, opts ...LocalOption) (string, error) {
	opt := LocalOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	ctx, release, err := module.begin(opt.Context)
	if err != nil {
		return "", err
	}
	defer release()
	inst, file, err := module.resolve(code)
	if err != nil {
		return "", err
	}
	local, ok := inst.conn.(Localer)
	if !ok {
		return "", ErrLocalUnsupported
	}
	opt.Context = ctx
	unlock := module.objects.lock(objectID(file), false)
	defer unlock()
	return local.Local(file, opt)
}
func Browse(code string, opts ...BrowseOption) (string, error) {
	opt := BrowseOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	ctx, release, err := module.begin(opt.Context)
	if err != nil {
		return "", err
	}
	defer release()
	inst, file, err := module.resolve(code)
	if err != nil {
		return "", err
	}
	browser, ok := inst.conn.(Browser)
	if !ok {
		return "", ErrBrowseUnsupported
	}
	opt.Context = ctx
	opt.Headers, opt.Params = support.CloneMap(opt.Headers), support.CloneMap(opt.Params)
	if err := validateBrowseOptions(inst.conn, opt); err != nil {
		return "", err
	}
	return browser.Browse(file, opt)
}
func Stat(code string, opts ...StatOption) (ObjectInfo, error) {
	opt := StatOption{}
	if len(opts) > 0 {
		opt = opts[0]
	}
	ctx, release, err := module.begin(opt.Context)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer release()
	inst, file, err := module.resolve(code)
	if err != nil {
		return ObjectInfo{}, err
	}
	statter, ok := inst.conn.(Statter)
	if !ok {
		return ObjectInfo{}, ErrStatUnsupported
	}
	opt.Context = ctx
	return statter.Stat(file, opt)
}
