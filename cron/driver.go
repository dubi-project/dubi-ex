package cron

import (
	"time"
)

type (
	Driver interface {
		Connection(*Instance) (Connection, error)
	}

	Connection interface {
		Open() error
		Close() error

		// Add stores or updates one job definition.
		Add(name string, job Job) error
		// Enable marks one job as enabled.
		Enable(name string) error
		// Disable marks one job as disabled.
		Disable(name string) error
		// Remove deletes one job definition.
		Remove(name string) error
		// List returns all persisted job definitions.
		List() (map[string]Job, error)
		// Append writes one execution log entry.
		AppendLog(log Log) error
		// History returns logs of one job, with total count and paged results.
		History(jobName string, offset, limit int) (int64, []Log, error)

		// Lock tries to acquire a distributed lock key with ttl.
		// It returns true when lock is acquired, false when already locked.
		Lock(key string, ttl time.Duration) (bool, error)
	}
)
