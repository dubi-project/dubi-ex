package mutex

import (
	"fmt"
	"strings"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	. "github.com/dubi-project/dubi-go/base"
)

type Config struct {
	Type            string
	Weight          int
	Prefix          string
	Expire          time.Duration
	CleanupInterval time.Duration
	TokenGrace      time.Duration
	Settings        Map
}
type Configs map[string]Config

func (Config) Scope() string  { return "mutex:config" }
func (Configs) Scope() string { return "mutex:config" }

func (m *Module) Config(global Map) error {
	files, err := support.Sources(global, "mutex", "expire", "cleanup_interval", "token_grace")
	if err != nil {
		return err
	}
	for _, name := range support.Keys(files) {
		if !validSource(name) {
			return fmt.Errorf("mutex: invalid source %q", name)
		}
		if _, err := parseConfig(files[name], Config{}); err != nil {
			return fmt.Errorf("mutex source %q: %w", name, err)
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return fmt.Errorf("mutex: config after open")
	}
	m.fileConfigs = files
	m.sources, m.ring = nil, nil
	return nil
}

func parseConfig(fields Map, cfg Config) (Config, error) {
	for _, key := range support.Keys(fields) {
		value := fields[key]
		switch key {
		case "type", "prefix":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("%s must be a string", key)
			}
			if key == "type" {
				cfg.Type = text
			} else {
				cfg.Prefix = text
			}
		case "weight":
			n, err := support.Integer(value)
			if err != nil || n < -1 || int64(int(n)) != n {
				return cfg, fmt.Errorf("invalid weight %v", value)
			}
			cfg.Weight = int(n)
		case "expire", "cleanup_interval", "token_grace":
			duration, err := support.Duration(value)
			if err != nil || duration < 0 {
				return cfg, fmt.Errorf("invalid %s", key)
			}
			switch key {
			case "expire":
				cfg.Expire = duration
			case "cleanup_interval":
				cfg.CleanupInterval = duration
			case "token_grace":
				cfg.TokenGrace = duration
			}
		case "setting", "settings":
		default:
			return cfg, fmt.Errorf("unknown config field %q", key)
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
	if cfg.Weight == 0 {
		cfg.Weight = 1
	}
	if cfg.Weight < -1 {
		return cfg, fmt.Errorf("invalid weight %d", cfg.Weight)
	}
	if cfg.Expire < 0 || cfg.CleanupInterval < 0 || cfg.TokenGrace < 0 {
		return cfg, ErrInvalidLease
	}
	if cfg.Expire == 0 {
		cfg.Expire = time.Second
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = 30 * time.Second
	}
	if cfg.TokenGrace == 0 {
		cfg.TokenGrace = time.Minute
	}
	return cfg, nil
}

func validSource(name string) bool {
	return name != "" && strings.TrimSpace(name) == name && !strings.ContainsRune(name, 0)
}
