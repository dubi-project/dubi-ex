// Package cache provides driver-backed caching mounted on the dubi-go runtime.
// Without configuration it uses a bounded, process-local memory cache.
package cache

import dubi "github.com/dubi-project/dubi-go"

var (
	module = &Module{}
	host   dubi.Host
)

func init() {
	module.RegisterDriver("memory", &memoryDriver{})
	module.RegisterDriver("default", &memoryDriver{})
	host = dubi.Mount(module)
}

// Host returns the runtime host assigned to this module.
func Host() dubi.Host { return host }

// RegisterDriver makes a driver available for Config.Type. Driver subpackages
// normally call this from init; importing a driver never opens a connection.
func RegisterDriver(name string, driver Driver) { module.RegisterDriver(name, driver) }
