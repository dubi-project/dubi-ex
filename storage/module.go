package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

type Module struct {
	mu                               sync.RWMutex
	opened, started, closing, closed bool
	configs, sources                 Configs
	fileConfigs                      map[string]Map
	drivers                          map[string]Driver
	instances                        map[string]*Instance
	ring                             *support.HashRing
	options                          Options
	cache                            map[string]*os.Root
	ctx                              context.Context
	cancel                           context.CancelFunc
	ops                              sync.WaitGroup
	objects                          objectLocks
	thumbnailers                     map[string]Thumbnailer
	previewers                       map[string]Previewer
}

func newModule() *Module {
	m := &Module{configs: Configs{}, drivers: map[string]Driver{}, instances: map[string]*Instance{}, options: defaultOptions()}
	m.resetHandlers()
	return m
}
func (m *Module) Setup(*dubi.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened || m.closing {
		return fmt.Errorf("storage: setup after open")
	}
	sources := Configs{}
	for name, cfg := range m.configs {
		sources[name] = cfg
	}
	for _, name := range support.Keys(m.fileConfigs) {
		cfg, err := parseConfig(m.fileConfigs[name], sources[name])
		if err != nil {
			return err
		}
		sources[name] = cfg
	}
	if len(sources) == 0 {
		sources["default"] = Config{}
	}
	weights := map[string]int{}
	for _, name := range support.Keys(sources) {
		cfg, err := parseConfig(nil, sources[name])
		if err != nil {
			return err
		}
		if m.drivers[cfg.Type] == nil {
			return fmt.Errorf("storage source %q: unknown driver %q", name, cfg.Type)
		}
		sources[name] = cfg
		if cfg.Weight > 0 {
			weights[name] = cfg.Weight
		}
	}
	ring, err := support.NewHashRing(weights)
	if err != nil {
		return err
	}
	m.sources, m.ring = sources, ring
	return nil
}
func (m *Module) Open(*dubi.Context) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened {
		return nil
	}
	if m.closing || m.sources == nil {
		return fmt.Errorf("storage: setup required")
	}
	m.cache = map[string]*os.Root{}
	defer func() {
		if err != nil {
			for _, root := range m.cache {
				_ = root.Close()
			}
			m.cache = nil
			names := support.Keys(m.instances)
			for i := len(names) - 1; i >= 0; i-- {
				_ = m.instances[names[i]].conn.Close()
			}
			m.instances = map[string]*Instance{}
		}
	}()
	for _, name := range support.Keys(m.sources) {
		cfg := m.sources[name]
		inst := &Instance{Name: name, Config: cfg, Settings: support.CloneMap(cfg.Settings)}
		conn, e := m.drivers[cfg.Type].Connect(inst)
		if e != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return fmt.Errorf("storage source %q: connect: %w", name, e)
		}
		if conn == nil {
			return fmt.Errorf("storage source %q: nil connection", name)
		}
		inst.conn = conn
		m.instances[name] = inst
		if e = conn.Open(); e != nil {
			return fmt.Errorf("storage source %q: open: %w", name, e)
		}
	}
	for kind, dir := range map[string]string{"download": m.options.Download, "thumbnail": m.options.Thumbnail, "preview": m.options.Preview} {
		dir, err = filepath.Abs(dir)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		root, e := os.OpenRoot(dir)
		if e != nil {
			return e
		}
		m.cache[kind] = root
	}
	m.options.Temp, err = filepath.Abs(m.options.Temp)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(m.options.Temp, 0755); err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.opened, m.closed = true, false
	return nil
}
func (m *Module) Start(*dubi.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.opened {
		return ErrNotOpen
	}
	m.started = true
	return nil
}
func (m *Module) Stop(*dubi.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}
func (m *Module) Close(*dubi.Context) error {
	m.mu.Lock()
	if !m.opened {
		m.mu.Unlock()
		return nil
	}
	m.opened, m.started, m.closing, m.closed = false, false, true, true
	m.cancel()
	m.mu.Unlock()
	m.ops.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	names := support.Keys(m.instances)
	for i := len(names) - 1; i >= 0; i-- {
		errs = append(errs, m.instances[names[i]].conn.Close())
	}
	for _, root := range m.cache {
		errs = append(errs, root.Close())
	}
	m.instances = map[string]*Instance{}
	m.cache = nil
	m.closing = false
	return errors.Join(errs...)
}
func (m *Module) Reset() {
	_ = m.Close(nil)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = Configs{}
	m.sources = nil
	m.fileConfigs = nil
	m.ring = nil
	m.options = defaultOptions()
	m.ctx, m.cancel = nil, nil
	m.closed = false
	m.resetHandlers()
}
func (m *Module) begin(parent context.Context) (context.Context, func(), error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.opened {
		if m.closed {
			return nil, nil, ErrClosed
		}
		return nil, nil, ErrNotOpen
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(m.ctx, cancel)
	m.ops.Add(1)
	var once sync.Once
	return ctx, func() { once.Do(func() { stop(); cancel(); m.ops.Done() }) }, nil
}
func (m *Module) resolve(code string) (*Instance, *File, error) {
	file, err := Decode(code)
	if err != nil {
		return nil, nil, err
	}
	inst := m.instances[file.base]
	if inst == nil {
		return nil, nil, ErrInvalidConnection
	}
	file.proxy, file.remote = inst.Config.Proxy, inst.Config.Remote
	return inst, file, nil
}
