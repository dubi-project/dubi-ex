package storage

import (
	"reflect"
	"strings"

	"github.com/dubi-project/dubi-ex/internal/support"
)

func (m *Module) Register(name string, value any) {
	switch cfg := value.(type) {
	case Config:
		m.RegisterConfig(name, cfg)
	case *Config:
		if cfg != nil {
			m.RegisterConfig(name, *cfg)
		}
	case Configs:
		m.RegisterConfigs(cfg)
	case *Configs:
		if cfg != nil {
			m.RegisterConfigs(*cfg)
		}
	case Driver:
		m.RegisterDriver(name, cfg)
	case Thumbnailer:
		m.RegisterThumbnailer(name, cfg)
	case *Thumbnailer:
		if cfg != nil {
			m.RegisterThumbnailer(name, *cfg)
		}
	case Previewer:
		m.RegisterPreviewer(name, cfg)
	case *Previewer:
		if cfg != nil {
			m.RegisterPreviewer(name, *cfg)
		}
	}
}
func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened || m.closing {
		panic("storage: register driver after open")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "default"
	}
	if driver == nil || (reflect.ValueOf(driver).Kind() == reflect.Pointer && reflect.ValueOf(driver).IsNil()) {
		panic("storage: nil driver")
	}
	m.drivers[name] = driver
}
func (m *Module) RegisterConfig(name string, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened || m.closing {
		panic("storage: register config after open")
	}
	if name == "" {
		name = "default"
	}
	if !validSource(name) {
		panic("storage: invalid source " + name)
	}
	cfg.Settings = support.CloneMap(cfg.Settings)
	m.configs[name] = cfg
}
func (m *Module) RegisterConfigs(configs Configs) {
	for _, name := range support.Keys(configs) {
		m.RegisterConfig(name, configs[name])
	}
}
