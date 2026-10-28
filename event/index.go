// Package event provides process-local messaging and optional external drivers.
package event

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
func Ready() bool {
	module.mutex.RLock()
	defer module.mutex.RUnlock()
	return module.started && !module.stopping
}
