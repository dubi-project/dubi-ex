package cron

import (
	"context"
	"fmt"
	"sync"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
	robcron "github.com/robfig/cron/v3"
)

type Module struct {
	mutex      sync.RWMutex
	config     Config
	defaults   Config
	fileConfig Map
	drivers    map[string]Driver
	jobs       map[string]Job
	instance   *Instance
	connect    Connection
	parser     robcron.Parser
	schedules  map[string][]*scheduleItem
	running    bool
	stopCh     chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}
type Instance struct {
	Config   Config
	Settings Map
}

func newModule() *Module {
	return &Module{drivers: map[string]Driver{}, jobs: map[string]Job{}, schedules: map[string][]*scheduleItem{}, parser: robcron.NewParser(robcron.SecondOptional | robcron.Minute | robcron.Hour | robcron.Dom | robcron.Month | robcron.Dow | robcron.Descriptor)}
}

func (m *Module) Setup(_ *dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		return fmt.Errorf("cron: setup after open")
	}
	cfg, err := parseConfig(m.fileConfig, m.defaults)
	if err != nil {
		return err
	}
	if m.drivers[cfg.Type] == nil {
		return fmt.Errorf("cron: unknown driver %q", cfg.Type)
	}
	for name, job := range m.jobs {
		if err := m.validateJob(name, job); err != nil {
			return err
		}
	}
	m.config = cfg
	return nil
}
func (m *Module) Open(_ *dubi.Context) (err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		return nil
	}
	driver := m.drivers[m.config.Type]
	if driver == nil {
		return fmt.Errorf("cron: setup required")
	}
	inst := &Instance{Config: m.config, Settings: cloneMap(m.config.Settings)}
	conn, err := driver.Connection(inst)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return err
	}
	if conn == nil {
		return fmt.Errorf("cron: nil connection")
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	if err = conn.Open(); err != nil {
		return err
	}
	stored, err := conn.List()
	if err != nil {
		return err
	}
	jobs := map[string]Job{}
	for name, job := range stored {
		job.Name = name
		if err = m.validateJob(name, job); err != nil {
			return err
		}
		jobs[name] = cloneJob(job)
	}
	for name, job := range m.jobs {
		if err = conn.Add(name, job); err != nil {
			return err
		}
		jobs[name] = cloneJob(job)
	}
	m.jobs = jobs
	m.instance = inst
	m.connect = conn
	m.rebuildSchedulesLocked()
	return nil
}
func (m *Module) Start(_ *dubi.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.running {
		return nil
	}
	if m.connect == nil {
		return fmt.Errorf("cron: not open")
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.stopCh = make(chan struct{})
	m.running = true
	m.wg.Add(1)
	go m.loop(m.config.Tick, m.config.Sync, m.stopCh)
	return nil
}
func (m *Module) Stop(_ *dubi.Context) error {
	m.mutex.Lock()
	if !m.running {
		m.mutex.Unlock()
		return nil
	}
	m.running = false
	close(m.stopCh)
	m.cancel()
	m.mutex.Unlock()
	m.wg.Wait()
	return nil
}
func (m *Module) Close(_ *dubi.Context) error {
	_ = m.Stop(nil)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	var err error
	if m.connect != nil {
		err = m.connect.Close()
	}
	m.connect = nil
	m.instance = nil
	m.schedules = map[string][]*scheduleItem{}
	return err
}
func (m *Module) Reset() {
	_ = m.Close(nil)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.config = Config{}
	m.defaults = Config{}
	m.fileConfig = nil
	m.jobs = map[string]Job{}
}

type Job struct {
	Name      string   `json:"name"`
	Desc      string   `json:"desc"`
	Target    string   `json:"target"`
	Schedule  string   `json:"schedule"`
	Schedules []string `json:"schedules"`
	Disabled  bool     `json:"disabled"`
	Payload   Map      `json:"payload"`
	Settings  Map      `json:"settings"`
}
type Jobs map[string]Job
type Log struct {
	Job       string    `json:"job"`
	Schedule  string    `json:"schedule"`
	Target    string    `json:"target"`
	Payload   Map       `json:"payload"`
	Triggered time.Time `json:"triggered"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended"`
	Success   bool      `json:"success"`
	State     string    `json:"state"`
	Error     string    `json:"error"`
}
type scheduleItem struct {
	spec     string
	schedule robcron.Schedule
	next     time.Time
}
