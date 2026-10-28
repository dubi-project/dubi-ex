package queue

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dubi-project/dubi-ex/internal/support"
)

func (Queue) Scope() string   { return "queue:queue" }
func (Queues) Scope() string  { return "queue:queue" }
func (Declare) Scope() string { return "queue:declare" }
func (Filter) Scope() string  { return "queue:filter" }
func (Handler) Scope() string { return "queue:handler" }
func (m *Module) Register(name string, value any) {
	switch v := value.(type) {
	case Queue:
		m.RegisterQueue(name, v)
	case *Queue:
		if v != nil {
			m.RegisterQueue(name, *v)
		}
	case Queues:
		m.RegisterQueues(name, v)
	case *Queues:
		if v != nil {
			m.RegisterQueues(name, *v)
		}
	case Declare:
		m.RegisterDeclare(name, v)
	case *Declare:
		if v != nil {
			m.RegisterDeclare(name, *v)
		}
	case Filter:
		m.RegisterFilter(name, v)
	case *Filter:
		if v != nil {
			m.RegisterFilter(name, *v)
		}
	case Handler:
		m.RegisterHandler(name, v)
	case *Handler:
		if v != nil {
			m.RegisterHandler(name, *v)
		}
	case Config:
		m.RegisterConfig(name, v)
	case *Config:
		if v != nil {
			m.RegisterConfig(name, *v)
		}
	case Configs:
		m.RegisterConfigs(v)
	case *Configs:
		if v != nil {
			m.RegisterConfigs(*v)
		}
	case Driver:
		m.RegisterDriver(name, v)
	}
}
func (m *Module) RegisterQueue(name string, cfg Queue) {
	cfg.Settings = support.CloneMap(cfg.Settings)
	cfg.Args = cfg.Args.Clone()
	cfg.Alias = append([]string(nil), cfg.Alias...)
	cfg.Actions = append([]ctxFunc(nil), cfg.Actions...)
	if cfg.Thread <= 0 {
		cfg.Thread = 1
	}
	cfg.Retry = append(Retry(nil), cfg.Retry...)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("queue: register after open")
	}
	for _, key := range append([]string{name}, cfg.Alias...) {
		if err := validateName(key); err != nil {
			panic(err)
		}
		m.queues[key] = cfg
		m.declares[key] = Declare{Alias: append([]string(nil), cfg.Alias...), Name: cfg.Name, Desc: cfg.Desc, Nullable: cfg.Nullable, Args: cfg.Args.Clone()}
	}
}
func (m *Module) RegisterQueues(prefix string, values Queues) {
	for _, name := range support.Keys(values) {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		m.RegisterQueue(key, values[name])
	}
}
func (m *Module) RegisterDeclare(name string, cfg Declare) {
	cfg.Args = cfg.Args.Clone()
	cfg.Alias = append([]string(nil), cfg.Alias...)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("queue: register after open")
	}
	for _, key := range append([]string{name}, cfg.Alias...) {
		if err := validateName(key); err != nil {
			panic(err)
		}
		m.declares[key] = cfg
	}
}
func (m *Module) RegisterFilter(name string, cfg Filter) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("queue: register after open")
	}
	m.filters[name] = cfg
}
func (m *Module) RegisterHandler(name string, cfg Handler) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("queue: register after open")
	}
	m.handlers[name] = cfg
}
func (m *Module) RegisterConfig(name string, cfg Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		panic("queue: register config after open")
	}
	if name == "" {
		name = "default"
	}
	cfg.Settings = support.CloneMap(cfg.Settings)
	m.configs[name] = cfg
}
func (m *Module) RegisterConfigs(values Configs) {
	for _, name := range support.Keys(values) {
		m.RegisterConfig(name, values[name])
	}
}
func validateName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidQueue, name)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("%w: %q", ErrInvalidQueue, name)
		}
	}

	return nil
}
