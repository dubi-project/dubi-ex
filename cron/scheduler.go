package cron

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	dubi "github.com/dubi-project/dubi-go"
)

func (m *Module) validateJob(name string, job Job) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("cron: job name is required")
	}
	if job.Target == "" {
		return fmt.Errorf("cron job %q: target is required", name)
	}
	specs := collectSpecs(job)
	if len(specs) == 0 {
		return fmt.Errorf("cron job %q: schedule is required", name)
	}
	for _, spec := range specs {
		schedule, err := m.parser.Parse(spec)
		if err != nil {
			return fmt.Errorf("cron job %q schedule %q: %w", name, spec, err)
		}
		if schedule.Next(time.Now()).IsZero() {
			return fmt.Errorf("cron job %q: schedule has no next occurrence", name)
		}
	}
	return nil
}
func (m *Module) rebuildSchedulesLocked() {
	schedules := map[string][]*scheduleItem{}
	now := time.Now()
	for name, job := range m.jobs {
		if job.Disabled {
			continue
		}
		for _, spec := range collectSpecs(job) {
			schedule, err := m.parser.Parse(spec)
			if err != nil {
				continue
			}
			next := schedule.Next(now)
			if next.IsZero() {
				continue
			}
			schedules[name] = append(schedules[name], &scheduleItem{spec: spec, schedule: schedule, next: next})
		}
	}
	m.schedules = schedules
}
func (m *Module) loop(tick, syncEvery time.Duration, stop <-chan struct{}) {
	defer m.wg.Done()
	if !waitAlignedTick(tick, stop) {
		return
	}
	m.dispatchDue(time.Now())
	ticker := time.NewTicker(tick)
	syncTicker := time.NewTicker(syncEvery)
	defer ticker.Stop()
	defer syncTicker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.dispatchDue(time.Now())
		case <-syncTicker.C:
			m.syncFromStore()
		}
	}
}
func waitAlignedTick(tick time.Duration, stop <-chan struct{}) bool {
	if tick <= 0 {
		return true
	}
	now := time.Now()
	timer := time.NewTimer(now.Truncate(tick).Add(tick).Sub(now))
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-timer.C:
		return true
	}
}
func (m *Module) dispatchDue(now time.Time) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if !m.running || m.connect == nil {
		return
	}
	for name, items := range m.schedules {
		job := m.jobs[name]
		if job.Disabled {
			continue
		}
		for _, item := range items {
			if item.next.After(now) {
				continue
			}
			// Skip missed ticks rather than generating an unbounded catch-up
			// burst after a suspended process or a slow backing store.
			dueTime := item.next
			next := item.schedule.Next(now)
			if next.IsZero() || !next.After(now) {
				item.next = now.Add(24 * time.Hour)
				continue
			}
			item.next = next
			conn := m.connect
			job := cloneJob(job)
			spec := item.spec
			ttl := m.config.LockTTL
			requestCtx := m.ctx
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				key := fmt.Sprintf("cron:%s:%s:%d", job.Name, spec, dueTime.Unix())
				locked, err := conn.Lock(key, ttl)
				if err != nil || !locked {
					if err != nil {
						log.Printf("dubi-ex cron: lock %q: %v", job.Name, err)
					}
					return
				}
				started := time.Now()
				meta := dubi.NewMeta().WithContext(requestCtx)
				payload := cloneMap(job.Payload)
				meta.Invoke(job.Target, payload)
				res := meta.Result()
				entry := Log{Job: job.Name, Schedule: spec, Target: job.Target, Payload: cloneMap(payload), Triggered: dueTime, Started: started, Ended: time.Now(), Success: res.OK(), State: res.Status()}
				if res.Fail() {
					entry.Error = res.Error()
				}
				if err := conn.AppendLog(entry); err != nil {
					log.Printf("dubi-ex cron: history %q: %v", job.Name, err)
				}
			}()
		}
	}
}
func (m *Module) syncFromStore() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect == nil || !m.running {
		return
	}
	stored, err := m.connect.List()
	if err != nil {
		log.Printf("dubi-ex cron: sync: %v", err)
		return
	}
	latest := map[string]Job{}
	for name, job := range stored {
		job.Name = name
		if err := m.validateJob(name, job); err != nil {
			log.Printf("dubi-ex cron: sync: %v", err)
			return
		}
		latest[name] = cloneJob(job)
	}
	if !reflect.DeepEqual(latest, m.jobs) {
		m.jobs = latest
		m.rebuildSchedulesLocked()
	}
}
