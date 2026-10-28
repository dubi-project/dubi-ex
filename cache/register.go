package cache

// Register receives registrations selected by the dubi-go Runtime.
func (m *Module) Register(name string, value any) {
	switch item := value.(type) {
	case Config:
		m.RegisterConfig(name, item)
	case *Config:
		if item != nil {
			m.RegisterConfig(name, *item)
		}
	case Configs:
		m.RegisterConfigs(item)
	case *Configs:
		if item != nil {
			m.RegisterConfigs(*item)
		}
	case Driver:
		m.RegisterDriver(name, item)
	}
}
