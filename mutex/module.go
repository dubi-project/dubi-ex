package mutex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

type Module struct {
	mutex                   sync.RWMutex
	token                   sync.Mutex
	opened, closed, started bool
	drivers                 map[string]Driver
	configs                 Configs
	sources                 Configs
	fileConfigs             map[string]Map
	instances               map[string]*Instance
	ring                    *support.HashRing
	tokens                  map[string][]tokenEntry
	stats                   mutexStats
	cleanupInterval         time.Duration
	cleanerStop             chan struct{}
	cleanerDone             chan struct{}
	ctx                     context.Context
	cancel                  context.CancelFunc
}
type tokenEntry struct {
	token string
	until time.Time
	grace time.Duration
}

func newModule() *Module {
	return &Module{drivers: map[string]Driver{}, configs: Configs{}, instances: map[string]*Instance{}, tokens: map[string][]tokenEntry{}}
}

func (m *Module) Setup(*dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.setup()
}
func (m *Module) setup() error {
	if m.opened {
		return nil
	}
	sources := Configs{}
	for name, cfg := range m.configs {
		sources[name] = cfg
	}
	for _, name := range support.Keys(m.fileConfigs) {
		cfg, err := parseConfig(m.fileConfigs[name], sources[name])
		if err != nil {
			return fmt.Errorf("mutex source %q: %w", name, err)
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
			return fmt.Errorf("mutex source %q: %w", name, err)
		}
		if m.drivers[cfg.Type] == nil {
			return fmt.Errorf("mutex source %q: unknown driver %q", name, cfg.Type)
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
	m.cleanupInterval = m.resolveCleanupIntervalLocked()
	return nil
}
func (m *Module) Open(*dubi.Context) (err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return nil
	}
	if err = m.setup(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			cancel()
			names := support.Keys(m.instances)
			for i := len(names) - 1; i >= 0; i-- {
				_ = m.instances[names[i]].conn.Close()
			}
			m.instances = map[string]*Instance{}
		}
	}()
	for _, name := range support.Keys(m.sources) {
		cfg := m.sources[name]
		inst := &Instance{Name: name, Config: cfg, Settings: support.CloneMap(cfg.Settings), ctx: ctx}
		conn, e := m.drivers[cfg.Type].Connect(inst)
		if e != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return fmt.Errorf("mutex source %q: connect: %w", name, e)
		}
		if conn == nil {
			return fmt.Errorf("mutex source %q: nil connection", name)
		}
		inst.conn = conn
		m.instances[name] = inst
		if e = conn.Open(); e != nil {
			return fmt.Errorf("mutex source %q: open: %w", name, e)
		}
	}
	m.ctx, m.cancel = ctx, cancel
	m.opened, m.closed = true, false
	m.startCleanerLocked()
	return nil
}
func (m *Module) Start(*dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if !m.opened {
		return ErrNotReady
	}
	m.started = true
	return nil
}

// Locks remain usable during Stop so other modules can release their locks.
func (m *Module) Stop(*dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.started = false
	return nil
}
func (m *Module) Close(*dubi.Context) error {
	m.mutex.Lock()
	if !m.opened {
		m.mutex.Unlock()
		return nil
	}
	m.opened, m.closed, m.started = false, true, false
	if m.cancel != nil {
		m.cancel()
	}
	stop, done := m.cleanerStop, m.cleanerDone
	m.cleanerStop, m.cleanerDone = nil, nil
	if stop != nil {
		close(stop)
	}
	instances := m.instances
	m.mutex.Unlock()
	// Cleaner bookkeeping and lease renewals may take m.mutex.RLock. Join
	// them without holding the write lock used to stop new operations.
	if done != nil {
		<-done
	}
	for _, inst := range instances {
		inst.keepAlive.Wait()
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	var errs []error
	names := support.Keys(instances)
	for i := len(names) - 1; i >= 0; i-- {
		errs = append(errs, instances[names[i]].conn.Close())
	}
	m.instances = map[string]*Instance{}
	m.ring = nil
	m.cleanupInterval = 0
	m.token.Lock()
	m.tokens = map[string][]tokenEntry{}
	m.token.Unlock()
	return errors.Join(errs...)
}
func (m *Module) Reset() {
	_ = m.Close(nil)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.configs = Configs{}
	m.sources = nil
	m.fileConfigs = nil
	m.opened, m.closed, m.started = false, false, false
	m.ctx, m.cancel = nil, nil
	m.stats.reset()
}
func (m *Module) Ready() bool { m.mutex.RLock(); defer m.mutex.RUnlock(); return m.opened && m.started }

func (m *Module) resolveCleanupIntervalLocked() time.Duration {
	interval := time.Duration(0)
	for _, cfg := range m.sources {
		if cfg.CleanupInterval > 0 && (interval == 0 || cfg.CleanupInterval < interval) {
			interval = cfg.CleanupInterval
		}
	}
	if interval == 0 {
		interval = 30 * time.Second
	}
	return interval
}
func (m *Module) startCleanerLocked() {
	stop, done := make(chan struct{}), make(chan struct{})
	m.cleanerStop, m.cleanerDone = stop, done
	interval := m.cleanupInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.cleanupExpiredTokens()
			}
		}
	}()
}
