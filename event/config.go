package event

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	. "github.com/dubi-project/dubi-go/base"
)

var (
	ErrInvalidEvent   = errors.New("event: invalid event name")
	ErrNoConnection   = errors.New("event: invalid connection")
	ErrNotRunning     = errors.New("event: not running")
	ErrQueueFull      = errors.New("event: buffer full")
	ErrInvalidPayload = errors.New("event: invalid payload")
)

type PayloadError struct {
	Event  string
	Result Res
}

func (e *PayloadError) Error() string {
	if e == nil || e.Result == nil {
		return ErrInvalidPayload.Error()
	}
	return fmt.Sprintf("%s: %s", e.Event, e.Result.Error())
}
func (*PayloadError) Unwrap() error { return ErrInvalidPayload }

type Configs map[string]Config

func (Config) Scope() string  { return "event:config" }
func (Configs) Scope() string { return "event:config" }

type Config struct {
	Type         string
	External     bool
	Codec        string
	Weight       int
	Prefix       string
	Settings     Map
	Workers      int
	Queue        int
	QueuePolicy  string
	DrainTimeout time.Duration
}

func (m *Module) Config(global Map) error {
	files, err := support.Sources(global, "event", "workers", "queue", "queue_policy", "drain_timeout")
	if err != nil {
		return err
	}
	for _, name := range support.Keys(files) {
		fields := files[name]
		if _, err := parseConfig(fields, Config{}); err != nil {
			return fmt.Errorf("event source %q: %w", name, err)
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return fmt.Errorf("event: config after open")
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
		case "workers", "queue":
			number, err := support.Integer(value)
			if err != nil || number < 0 || number > 1048576 {
				return cfg, fmt.Errorf("invalid %s", key)
			}
			if key == "workers" {
				cfg.Workers = int(number)
			} else {
				cfg.Queue = int(number)
			}
		case "queue_policy":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("queue_policy must be a string")
			}
			cfg.QueuePolicy = text
		case "drain_timeout":
			duration, err := support.Duration(value)
			if err != nil || duration < 0 {
				return cfg, fmt.Errorf("invalid drain_timeout")
			}
			cfg.DrainTimeout = duration
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
	cfg.Prefix = normalizePrefix(cfg.Prefix)
	if cfg.Workers < 0 || cfg.Workers > 1048576 || cfg.Queue < 0 || cfg.Queue > 1048576 || cfg.DrainTimeout < 0 {
		return cfg, fmt.Errorf("invalid worker configuration")
	}
	cfg.QueuePolicy = strings.ToLower(strings.TrimSpace(cfg.QueuePolicy))
	switch cfg.QueuePolicy {
	case "", "block", "drop", "sync", "async":
	default:
		return cfg, fmt.Errorf("invalid queue_policy %q", cfg.QueuePolicy)
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
