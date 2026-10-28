package queue

import (
	"time"
)

type (
	Driver interface {
		Connect(*Instance) (Connection, error)
	}

	Connection interface {
		Open() error
		Close() error
		Start() error
		Stop() error

		Register(name string) error
		Publish(name string, data []byte) error
		DeferredPublish(name string, data []byte, delay time.Duration) error
	}
)
