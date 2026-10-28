package mutex

import (
	"context"
	"sync"
	"time"

	base "github.com/dubi-project/dubi-go/base"
)

type (
	// Driver defines a mutex driver.
	Driver interface {
		Connect(*Instance) (Connection, error)
	}

	// Connection defines a mutex connection.
	Connection interface {
		Open() error
		Close() error

		Lock(key string, expires time.Duration) error
		Unlock(key string) error
	}

	Checker interface {
		Locked(key string) (bool, error)
	}

	Refresher interface {
		Refresh(key string, expires time.Duration) error
	}

	TokenConnection interface {
		LockToken(key string, expires time.Duration) (string, error)
		UnlockToken(key, token string) error
	}

	TokenRefresher interface {
		RefreshToken(key, token string, expires time.Duration) error
	}

	CapabilityProvider interface {
		Capabilities() Capability
	}

	Capability struct {
		Check        bool
		Refresh      bool
		Token        bool
		TokenRefresh bool
	}

	// Instance is the driver instance context.
	Instance struct {
		conn      Connection
		Name      string
		Config    Config
		Settings  base.Map
		stats     mutexStats
		ctx       context.Context
		keepAlive sync.WaitGroup
	}
)
