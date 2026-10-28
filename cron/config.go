package cron

import (
	"fmt"
	"strings"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	. "github.com/dubi-project/dubi-go/base"
)

type Config struct {
	Type     string
	Tick     time.Duration
	Sync     time.Duration
	LockTTL  time.Duration
	Settings Map
}

func (Config) Scope() string { return "cron:config" }
func (m *Module) Config(global Map) error {
	fields := Map{}
	if value, exists := global["cron"]; exists {
		var ok bool
		fields, ok = support.AsMap(value)
		if !ok {
			return fmt.Errorf("cron: config must be an object")
		}
	}
	if _, err := parseConfig(fields, Config{}); err != nil {
		return err
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.connect != nil {
		return fmt.Errorf("cron: config after open")
	}
	m.fileConfig = cloneMap(fields)
	return nil
}
func parseConfig(fields Map, cfg Config) (Config, error) {
	for _, key := range support.Keys(fields) {
		value := fields[key]
		switch key {
		case "type":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("cron: type must be string")
			}
			cfg.Type = text
		case "tick", "sync", "reload", "lockttl", "lock_ttl":
			duration, err := support.Duration(value)
			if err != nil || duration <= 0 {
				return cfg, fmt.Errorf("cron: invalid %s", key)
			}
			switch key {
			case "tick":
				cfg.Tick = duration
			case "sync", "reload":
				cfg.Sync = duration
			default:
				cfg.LockTTL = duration
			}
		case "settings", "setting":
		default:
			return cfg, fmt.Errorf("cron: unknown config field %q", key)
		}
	}
	var err error
	cfg.Settings, err = support.MergeSettings(cfg.Settings, fields)
	if err != nil {
		return cfg, err
	}
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	if cfg.Type == "" {
		cfg.Type = "memory"
	}
	if cfg.Tick == 0 {
		cfg.Tick = time.Second
	}
	if cfg.Sync == 0 {
		cfg.Sync = 5 * time.Second
	}
	if cfg.LockTTL == 0 {
		cfg.LockTTL = 30 * time.Second
	}
	if cfg.Tick < 0 || cfg.Sync < 0 || cfg.LockTTL < 0 {
		return cfg, fmt.Errorf("cron: durations must be positive")
	}
	return cfg, nil
}
func cloneMap(value Map) Map { return support.CloneMap(value) }
