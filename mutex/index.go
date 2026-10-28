// Package mutex provides leased locks with a default process-local memory
// driver and optional external driver packages.
package mutex

import dubi "github.com/dubi-project/dubi-go"

var module = newModule()
var host dubi.Host

func init() {
	module.RegisterDriver("memory", &defaultDriver{})
	module.RegisterDriver("default", &defaultDriver{})
	host = dubi.Mount(module)
}

func Host() dubi.Host                           { return host }
func RegisterDriver(name string, driver Driver) { module.RegisterDriver(name, driver) }
func Ready() bool                               { return module.Ready() }
