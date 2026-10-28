package mutex

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
	}
}
func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("mutex: register driver after open")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "default"
	}
	if driver == nil || (reflect.ValueOf(driver).Kind() == reflect.Pointer && reflect.ValueOf(driver).IsNil()) {
		panic("mutex: nil driver")
	}
	m.drivers[name] = driver
}
func (m *Module) RegisterConfig(name string, cfg Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("mutex: register config after open")
	}
	if name == "" {
		name = "default"
	}
	if !validSource(name) {
		panic("mutex: invalid source " + name)
	}
	cfg.Settings = support.CloneMap(cfg.Settings)
	m.configs[name] = cfg
}
func (m *Module) RegisterConfigs(configs Configs) {
	for _, name := range support.Keys(configs) {
		m.RegisterConfig(name, configs[name])
	}
}
