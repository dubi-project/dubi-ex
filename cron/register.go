package cron

import (
	"strings"

	"github.com/dubi-project/dubi-ex/internal/support"
)

func (Job) Scope() string  { return "cron:job" }
func (Jobs) Scope() string { return "cron:job" }
func (m *Module) Register(name string, value any) {
	switch cfg := value.(type) {
	case Job:
		m.RegisterJob(name, cfg)
	case *Job:
		if cfg != nil {
			m.RegisterJob(name, *cfg)
		}
	case Jobs:
		m.RegisterJobs(cfg)
	case *Jobs:
		if cfg != nil {
			m.RegisterJobs(*cfg)
		}
	case Config:
		m.RegisterConfig(cfg)
	case *Config:
		if cfg != nil {
			m.RegisterConfig(*cfg)
		}
	case Driver:
		m.RegisterDriver(name, cfg)
	}
}
func (m *Module) RegisterDriver(name string, driver Driver) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		panic("cron: register driver after open")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "default"
	}
	if driver == nil {
		panic("cron: nil driver")
	}
	m.drivers[name] = driver
}
func (m *Module) RegisterConfig(cfg Config) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		panic("cron: register config after open")
	}
	cfg.Settings = cloneMap(cfg.Settings)
	m.defaults = cfg
}
func (m *Module) RegisterJob(name string, job Job) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		panic("cron: register job after open; use Add")
	}
	job.Name = name
	m.jobs[name] = cloneJob(job)
}
func (m *Module) RegisterJobs(jobs Jobs) {
	for _, name := range support.Keys(jobs) {
		m.RegisterJob(name, jobs[name])
	}
}
