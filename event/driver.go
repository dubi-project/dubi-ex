package event

import ()

type (
	Driver interface {
		Connect(*Instance) (Connection, error)
	}

	Connection interface {
		Open() error
		Close() error
		Start() error
		Stop() error

		Register(name, group string) error
		Publish(name string, data []byte) error
	}
)
