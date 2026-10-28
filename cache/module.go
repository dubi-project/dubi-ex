package cache

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

// Module owns the process-wide sources, routing, and driver lifecycle.
type Module struct {
	mutex       sync.RWMutex
	prepared    bool
	opened      bool
	configs     Configs
	sources     Configs
	fileConfigs map[string]Map
	drivers     map[string]Driver
	instances   map[string]*Instance
	hashring    *hashRing
	stats       cacheStats
}

// Config describes one logical cache source. Type selects the driver; an empty
// Type uses memory. Weight 0 defaults to 1; -1 allows explicit access only.
type Config struct {
	Type          string
	Weight        int
	Prefix        string
	Codec         string
	Expire        time.Duration
	AllowClearAll bool
	Settings      Map
}

func (Config) Scope() string { return "cache:config" }

type Configs map[string]Config

func (Configs) Scope() string { return "cache:config" }

// Instance is one source connection, not a separate runtime or module.
type Instance struct {
	Name     string
	Config   Config
	Settings Map
	connect  Connect
	stats    cacheStats
}

func (m *Module) initMaps() {
	if m.configs == nil {
		m.configs = make(Configs)
	}
	if m.drivers == nil {
		m.drivers = make(map[string]Driver)
	}
	if m.instances == nil {
		m.instances = make(map[string]*Instance)
	}
}

func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.prepared {
		panic("cache: register driver after setup")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || nilValue(driver) {
		panic("cache: invalid driver " + name)
	}
	m.initMaps()
	m.drivers[name] = driver
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Slice, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

func (m *Module) RegisterConfig(name string, config Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.prepared {
		panic("cache: register config after setup")
	}
	m.initMaps()
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultName
	}
	config.Settings = cloneMap(config.Settings)
	m.configs[name] = config
}

func (m *Module) RegisterConfigs(configs Configs) {
	for _, name := range sortedKeys(configs) {
		m.RegisterConfig(name, configs[name])
	}
}

func (m *Module) Setup(*dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.setup()
}

func (m *Module) setup() error {
	if m.prepared {
		return nil
	}
	m.initMaps()
	configs := make(Configs, len(m.configs)+len(m.fileConfigs))
	for name, cfg := range m.configs {
		configs[name] = cfg
	}
	for _, name := range sortedKeys(m.fileConfigs) {
		cfg, err := parseConfig(m.fileConfigs[name], configs[name])
		if err != nil {
			return fmt.Errorf("cache source %q: %w", name, err)
		}
		configs[name] = cfg
	}
	if len(configs) == 0 {
		configs[defaultName] = Config{}
	}
	weights := make(map[string]int, len(configs))
	for _, name := range sortedKeys(configs) {
		cfg, err := normalizeConfig(configs[name])
		if err != nil {
			return fmt.Errorf("cache source %q: %w", name, err)
		}
		if nilValue(m.drivers[cfg.Type]) {
			return fmt.Errorf("cache source %q: unknown driver %q", name, cfg.Type)
		}
		configs[name] = cfg
		if cfg.Weight > 0 {
			weights[name] = cfg.Weight
		}
	}
	ring, err := newHashRing(weights)
	if err != nil {
		return err
	}
	m.sources, m.hashring, m.prepared = configs, ring, true
	return nil
}

func (m *Module) Open(*dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return nil
	}
	if err := m.setup(); err != nil {
		return err
	}
	instances := make(map[string]*Instance, len(m.sources))
	var opened []Connect
	rollback := func(err error) error {
		for i := len(opened) - 1; i >= 0; i-- {
			err = errors.Join(err, opened[i].Close())
		}
		return err
	}
	for _, name := range sortedKeys(m.sources) {
		cfg := m.sources[name]
		cfg.Settings = cloneMap(cfg.Settings)
		inst := &Instance{Name: name, Config: cfg, Settings: cloneMap(cfg.Settings)}
		conn, err := m.drivers[cfg.Type].Connect(inst)
		if err != nil {
			return rollback(fmt.Errorf("cache source %q: connect: %w", name, err))
		}
		if nilValue(conn) {
			return rollback(fmt.Errorf("cache source %q: driver returned nil connection", name))
		}
		opened = append(opened, conn)
		if err := conn.Open(); err != nil {
			return rollback(fmt.Errorf("cache source %q: open: %w", name, err))
		}
		inst.connect = conn
		instances[name] = inst
	}
	m.instances, m.opened = instances, true
	return nil
}

func (*Module) Start(*dubi.Context) error { return nil }
func (*Module) Stop(*dubi.Context) error  { return nil }

func (m *Module) Close(*dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	var err error
	names := sortedKeys(m.instances)
	for i := len(names) - 1; i >= 0; i-- {
		name := names[i]
		if closeErr := m.instances[name].connect.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("cache source %q: close: %w", name, closeErr))
		}
	}
	m.instances = make(map[string]*Instance)
	m.sources = nil
	m.opened, m.prepared, m.hashring = false, false, nil
	return err
}

// Reset clears source configuration and statistics. Installed drivers survive
// dubi.Reset so driver subpackages registered by init remain available.
func (m *Module) Reset() {
	_ = m.Close(nil)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.configs = make(Configs)
	m.fileConfigs = nil
	m.stats.reset()
}

func (m *Module) Ready() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.opened && len(m.instances) > 0
}

func (m *Module) Health(context.Context) error {
	if !m.Ready() {
		return ErrNotOpen
	}
	return nil
}

func Ready() bool { return module.Ready() }

func sortedKeys[T any](values map[string]T) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
