package cron

import "fmt"

func (m *Module) Upsert(name string, job Job) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if err := m.validateJob(name, job); err != nil {
		return err
	}
	job.Name = name
	job = cloneJob(job)
	if m.connect != nil {
		if err := m.connect.Add(name, job); err != nil {
			return err
		}
	}
	m.jobs[name] = job
	m.rebuildSchedulesLocked()
	return nil
}
func (m *Module) Delete(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		if err := m.connect.Remove(name); err != nil {
			return err
		}
	}
	delete(m.jobs, name)
	delete(m.schedules, name)
	return nil
}
func (m *Module) Enable(name string) error  { return m.setDisabled(name, false) }
func (m *Module) Disable(name string) error { return m.setDisabled(name, true) }
func (m *Module) setDisabled(name string, disabled bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	job, ok := m.jobs[name]
	if !ok {
		return fmt.Errorf("cron: job %q not found", name)
	}
	if job.Disabled == disabled {
		return nil
	}
	if m.connect != nil {
		var err error
		if disabled {
			err = m.connect.Disable(name)
		} else {
			err = m.connect.Enable(name)
		}
		if err != nil {
			return err
		}
	}
	job.Disabled = disabled
	m.jobs[name] = job
	m.rebuildSchedulesLocked()
	return nil
}
func (m *Module) ListLogs(name string, offset, limit int) (int64, []Log) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if m.connect == nil {
		return 0, []Log{}
	}
	total, logs, err := m.connect.History(name, offset, limit)
	if err != nil {
		return 0, []Log{}
	}
	return total, logs
}
