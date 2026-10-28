// Package storage provides object storage with a built-in local filesystem
// driver, process lifecycle integration, and optional external driver packages.
package storage

import dubi "github.com/dubi-project/dubi-go"

var module = newModule()
var host dubi.Host

func init() {
	for _, name := range []string{"file", "local", "default"} {
		module.RegisterDriver(name, &fileDriver{})
	}
	host = dubi.Mount(module)
}
func Host() dubi.Host                           { return host }
func RegisterDriver(name string, driver Driver) { module.RegisterDriver(name, driver) }
func Ready() bool {
	module.mu.RLock()
	defer module.mu.RUnlock()
	return module.opened && module.started
}
