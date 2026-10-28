package queue

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

type Module struct {
	mutex                                                         sync.RWMutex
	opened, started, stopping                                     bool
	configs                                                       Configs
	sources                                                       Configs
	fileConfigs                                                   map[string]Map
	drivers                                                       map[string]Driver
	queues                                                        map[string]Queue
	declares                                                      map[string]Declare
	filters                                                       map[string]Filter
	handlers                                                      map[string]Handler
	serveFilters, requestFilters, executeFilters, responseFilters []ctxFunc
	foundHandlers, errorHandlers, failedHandlers, deniedHandlers  []ctxFunc
	instances                                                     map[string]*Instance
	hashring                                                      *support.HashRing
}

func newModule() *Module {
	return &Module{
		configs: Configs{}, drivers: map[string]Driver{}, instances: map[string]*Instance{},
		queues: map[string]Queue{}, declares: map[string]Declare{}, filters: map[string]Filter{}, handlers: map[string]Handler{},
	}
}
func (m *Module) Reset() {
	_ = m.Close(nil)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	drivers := m.drivers
	m.configs = Configs{}
	m.sources = nil
	m.fileConfigs = nil
	m.instances = map[string]*Instance{}
	m.queues = map[string]Queue{}
	m.declares = map[string]Declare{}
	m.filters = map[string]Filter{}
	m.handlers = map[string]Handler{}
	m.serveFilters = nil
	m.requestFilters = nil
	m.executeFilters = nil
	m.responseFilters = nil
	m.foundHandlers = nil
	m.errorHandlers = nil
	m.failedHandlers = nil
	m.deniedHandlers = nil
	m.hashring = nil
	m.drivers = drivers
}
func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("queue: register driver after open")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "default"
	}
	if driver == nil || (reflect.ValueOf(driver).Kind() == reflect.Pointer && reflect.ValueOf(driver).IsNil()) {
		panic("queue: nil driver")
	}
	m.drivers[name] = driver
}
func (m *Module) Setup(_ *dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return fmt.Errorf("queue: setup after open")
	}
	sources := Configs{}
	for name, cfg := range m.configs {
		cfg.Settings = support.CloneMap(cfg.Settings)
		sources[name] = cfg
	}
	for _, name := range support.Keys(m.fileConfigs) {
		cfg, err := parseConfig(m.fileConfigs[name], sources[name])
		if err != nil {
			return fmt.Errorf("queue source %q: %w", name, err)
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
			return fmt.Errorf("queue source %q: %w", name, err)
		}
		if m.drivers[cfg.Type] == nil {
			return fmt.Errorf("queue: unknown driver %q", cfg.Type)
		}
		if _, err := dubi.Marshal(cfg.Codec, Map{}); err != nil {
			return fmt.Errorf("queue source %q: %w", name, err)
		}
		sources[name] = cfg
		if cfg.Weight > 0 {
			weights[name] = cfg.Weight
		}
	}
	for name, cfg := range m.queues {
		if cfg.Connect != "" && cfg.Connect != "*" {
			if _, ok := sources[cfg.Connect]; !ok {
				return fmt.Errorf("queue %q: unknown source %q", name, cfg.Connect)
			}
		}
	}
	ring, err := support.NewHashRing(weights)
	if err != nil {
		return err
	}
	m.sources = sources
	m.hashring = ring
	m.serveFilters = nil
	m.requestFilters = nil
	m.executeFilters = nil
	m.responseFilters = nil
	for _, name := range support.Keys(m.filters) {
		f := m.filters[name]
		if f.Serve != nil {
			m.serveFilters = append(m.serveFilters, f.Serve)
		}
		if f.Request != nil {
			m.requestFilters = append(m.requestFilters, f.Request)
		}
		if f.Execute != nil {
			m.executeFilters = append(m.executeFilters, f.Execute)
		}
		if f.Response != nil {
			m.responseFilters = append(m.responseFilters, f.Response)
		}
	}
	m.foundHandlers = nil
	m.errorHandlers = nil
	m.failedHandlers = nil
	m.deniedHandlers = nil
	for _, name := range support.Keys(m.handlers) {
		h := m.handlers[name]
		if h.Found != nil {
			m.foundHandlers = append(m.foundHandlers, h.Found)
		}
		if h.Error != nil {
			m.errorHandlers = append(m.errorHandlers, h.Error)
		}
		if h.Failed != nil {
			m.failedHandlers = append(m.failedHandlers, h.Failed)
		}
		if h.Denied != nil {
			m.deniedHandlers = append(m.deniedHandlers, h.Denied)
		}
	}
	return nil
}
func (m *Module) Open(_ *dubi.Context) (err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return nil
	}
	if m.sources == nil {
		return fmt.Errorf("queue: setup required")
	}
	defer func() {
		if err != nil {
			names := support.Keys(m.instances)
			for i := len(names) - 1; i >= 0; i-- {
				inst := m.instances[names[i]]
				inst.cancelContext()
				_ = inst.conn.Close()
			}
			m.instances = map[string]*Instance{}
		}
	}()
	for _, name := range support.Keys(m.sources) {
		cfg := m.sources[name]
		inst := &Instance{Name: name, Config: cfg, Settings: support.CloneMap(cfg.Settings), module: m}
		inst.resetContext()
		conn, e := m.drivers[cfg.Type].Connect(inst)
		if e != nil {
			if conn != nil {
				_ = conn.Close()
			}
			inst.cancelContext()
			return fmt.Errorf("queue source %q: connect: %w", name, e)
		}
		if conn == nil {
			inst.cancelContext()
			return fmt.Errorf("queue source %q: nil connection", name)
		}
		inst.conn = conn
		m.instances[name] = inst
		if e = conn.Open(); e != nil {
			return fmt.Errorf("queue source %q: open: %w", name, e)
		}
		for _, qName := range support.Keys(m.queues) {
			q := m.queues[qName]
			if q.Connect != "" && q.Connect != "*" && q.Connect != name {
				continue
			}
			for i := 0; i < q.Thread; i++ {
				if e = conn.Register(cfg.Prefix + qName); e != nil {
					return fmt.Errorf("queue register %q: %w", qName, e)
				}
			}
		}
	}
	m.opened = true
	return nil
}
func (m *Module) Start(_ *dubi.Context) (err error) {
	m.mutex.Lock()
	if m.started {
		m.mutex.Unlock()
		return nil
	}
	if !m.opened || m.stopping {
		m.mutex.Unlock()
		return fmt.Errorf("queue: not open")
	}
	names := support.Keys(m.instances)
	instances := m.instances
	m.mutex.Unlock()
	started := []*Instance{}
	defer func() {
		if err != nil {
			for i := len(started) - 1; i >= 0; i-- {
				inst := started[i]
				inst.cancelContext()
				_ = inst.conn.Stop()
				inst.stopWorkers()
			}
		}
	}()
	for _, name := range names {
		inst := instances[name]
		inst.resetContext()
		inst.startWorkers()
		if e := inst.conn.Start(); e != nil {
			inst.cancelContext()
			_ = inst.conn.Stop()
			inst.stopWorkers()
			return fmt.Errorf("queue source %q: start: %w", name, e)
		}
		started = append(started, inst)
	}
	m.mutex.Lock()
	m.started = true
	m.mutex.Unlock()
	return nil
}
func (m *Module) Stop(_ *dubi.Context) error {
	m.mutex.Lock()
	if !m.started {
		m.mutex.Unlock()
		return nil
	}
	m.started = false
	m.stopping = true
	instances := m.instances
	names := support.Keys(instances)
	m.mutex.Unlock()
	// Cancel every source before waiting: workers may be waiting on one another.
	for _, name := range names {
		instances[name].cancelContext()
	}
	var errs []error
	for i := len(names) - 1; i >= 0; i-- {
		inst := instances[names[i]]
		errs = append(errs, inst.conn.Stop())
		inst.stopWorkers()
	}
	m.mutex.Lock()
	m.stopping = false
	m.mutex.Unlock()
	return errors.Join(errs...)
}
func (m *Module) Close(_ *dubi.Context) error {
	stopErr := m.Stop(nil)
	m.mutex.Lock()
	instances := m.instances
	m.instances = map[string]*Instance{}
	m.opened = false
	m.mutex.Unlock()
	errs := []error{stopErr}
	names := support.Keys(instances)
	for i := len(names) - 1; i >= 0; i-- {
		inst := instances[names[i]]
		inst.cancelContext()
		if inst.conn != nil {
			errs = append(errs, inst.conn.Close())
		}
	}
	return errors.Join(errs...)
}
