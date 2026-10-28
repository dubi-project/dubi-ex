package queue

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dubi-project/dubi-ex/internal/support"
	. "github.com/dubi-project/dubi-go/base"
)

var (
	ErrInvalidQueue   = errors.New("queue: invalid queue name")
	ErrNoConnection   = errors.New("queue: invalid connection")
	ErrNotRunning     = errors.New("queue: not running")
	ErrQueueFull      = errors.New("queue: buffer full")
	ErrInvalidPayload = errors.New("queue: invalid payload")
)

type PayloadError struct {
	Queue  string
	Result Res
}

func (e *PayloadError) Error() string {
	if e == nil || e.Result == nil {
		return ErrInvalidPayload.Error()
	}
	return fmt.Sprintf("%s: %s", e.Queue, e.Result.Error())
}
func (*PayloadError) Unwrap() error { return ErrInvalidPayload }

type Configs map[string]Config

func (Config) Scope() string  { return "queue:config" }
func (Configs) Scope() string { return "queue:config" }

type Config struct {
	Type     string
	External bool
	Codec    string
	Weight   int
	Prefix   string
	Settings Map
}

func (m *Module) Config(global Map) error {
	files, err := support.Sources(global, "queue")
	if err != nil {
		return err
	}
	for _, name := range support.Keys(files) {
		fields := files[name]
		if _, err := parseConfig(fields, Config{}); err != nil {
			return fmt.Errorf("queue source %q: %w", name, err)
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return fmt.Errorf("queue: config after open")
	}
	m.fileConfigs = files
	m.sources = nil
	return nil
}
func parseConfig(fields Map, cfg Config) (Config, error) {
	for _, key := range support.Keys(fields) {
		value := fields[key]
		switch key {
		case "type", "codec", "prefix":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("%s must be a string", key)
			}
			switch key {
			case "type":
				cfg.Type = text
			case "codec":
				cfg.Codec = text
			case "prefix":
				cfg.Prefix = text
			}
		case "weight":
			number, err := support.Integer(value)
			if err != nil || number < -1 || int64(int(number)) != number {
				return cfg, fmt.Errorf("invalid weight %v", value)
			}
			cfg.Weight = int(number)
		case "external":
			flag, ok := value.(bool)
			if !ok {
				return cfg, fmt.Errorf("external must be boolean")
			}
			cfg.External = flag
		case "setting", "settings": // Merge once, below, with plural fields taking precedence.

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
	cfg.Codec = strings.ToLower(strings.TrimSpace(cfg.Codec))
	if cfg.Codec == "" {
		cfg.Codec = "json"
	}
	if cfg.Weight == 0 {
		cfg.Weight = 1
	}
	if cfg.Weight < -1 {
		return cfg, fmt.Errorf("invalid weight %d", cfg.Weight)
	}

	return cfg, nil
}

// Prefix is applied only at the transport boundary, not to business names.
func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || strings.HasSuffix(prefix, ".") || strings.HasSuffix(prefix, ":") || strings.HasSuffix(prefix, "/") {
		return prefix
	}
	return prefix + "."
}
