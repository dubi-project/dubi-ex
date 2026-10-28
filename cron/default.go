package cron

import (
	"sync"
	"time"
)

type (
	defaultDriver struct{}

	defaultConnect struct {
		mutex sync.Mutex

		jobs  map[string]Job
		locks map[string]time.Time
		logs  map[string][]Log
	}
)

func (d *defaultDriver) Connection(_ *Instance) (Connection, error) {
	return &defaultConnect{
		jobs:  make(map[string]Job),
		locks: make(map[string]time.Time),
		logs:  make(map[string][]Log),
	}, nil
}

func (c *defaultConnect) Open() error  { return nil }
func (c *defaultConnect) Close() error { return nil }

func (c *defaultConnect) List() (map[string]Job, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	out := make(map[string]Job, len(c.jobs))
	for name, job := range c.jobs {
		out[name] = cloneJob(job)
	}
	return out, nil
}

func (c *defaultConnect) Add(name string, job Job) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.jobs[name] = cloneJob(job)
	return nil
}

func (c *defaultConnect) Enable(name string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	job, ok := c.jobs[name]
	if !ok {
		return nil
	}
	job.Disabled = false
	c.jobs[name] = cloneJob(job)
	return nil
}

func (c *defaultConnect) Disable(name string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	job, ok := c.jobs[name]
	if !ok {
		return nil
	}
	job.Disabled = true
	c.jobs[name] = cloneJob(job)
	return nil
}

func (c *defaultConnect) Remove(name string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.jobs, name)
	delete(c.logs, name)
	return nil
}

func (c *defaultConnect) AppendLog(log Log) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	items := c.logs[log.Job]
	items = append(items, cloneLog(log))
	if len(items) > 10 {
		items = items[len(items)-10:]
	}
	c.logs[log.Job] = items
	return nil
}

func (c *defaultConnect) History(jobName string, offset, limit int) (int64, []Log, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	items := c.logs[jobName]
	if len(items) == 0 {
		return 0, []Log{}, nil
	}

	total := int64(len(items))
	if offset < 0 {
		offset = 0
	}
	startDesc := offset
	if startDesc > len(items) {
		startDesc = len(items)
	}
	endDesc := len(items)
	if limit > 0 && startDesc+limit < endDesc {
		endDesc = startDesc + limit
	}
	if startDesc > endDesc {
		startDesc = endDesc
	}

	out := make([]Log, 0, endDesc-startDesc)
	// Return logs in desc order: newest -> oldest.
	for i := startDesc; i < endDesc; i++ {
		idx := len(items) - 1 - i
		out = append(out, cloneLog(items[idx]))
	}
	return total, out, nil
}

func (c *defaultConnect) Lock(key string, ttl time.Duration) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()
	for name, expiresAt := range c.locks {
		if !now.Before(expiresAt) {
			delete(c.locks, name)
		}
	}
	if expiresAt, ok := c.locks[key]; ok && now.Before(expiresAt) {
		return false, nil
	}

	c.locks[key] = now.Add(ttl)
	return true, nil
}

func cloneJob(job Job) Job {
	out := job
	out.Payload = cloneMap(job.Payload)
	out.Settings = cloneMap(job.Settings)
	out.Schedules = cloneStrings(job.Schedules)
	return out
}

func cloneStrings(src []string) []string {
	if src == nil {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func cloneLog(log Log) Log {
	return Log{
		Job:       log.Job,
		Schedule:  log.Schedule,
		Target:    log.Target,
		Payload:   cloneMap(log.Payload),
		Triggered: log.Triggered,
		Started:   log.Started,
		Ended:     log.Ended,
		Success:   log.Success,
		State:     log.State,
		Error:     log.Error,
	}
}
