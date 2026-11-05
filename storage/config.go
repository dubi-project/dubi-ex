package storage

import (
	"fmt"
	"strings"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	. "github.com/dubi-project/dubi-go/base"
)

type Config struct {
	Type          string
	Weight        int
	Prefix        string
	Proxy, Remote bool
	Settings      Map
}
type Configs map[string]Config

func (Config) Scope() string  { return "storage:config" }
func (Configs) Scope() string { return "storage:config" }

type Options struct {
	Download           string
	Thumbnail          string
	Preview            string
	CacheTTL           time.Duration
	MaxImagePixels     int64
	MaxThumbnailPixels int64
}

func defaultOptions() Options {
	return Options{Download: "store/download", Thumbnail: "store/thumbnail", Preview: "store/preview", CacheTTL: 10 * time.Minute, MaxImagePixels: 40_000_000, MaxThumbnailPixels: 16_000_000}
}

func (m *Module) Config(global Map) error {
	options := defaultOptions()
	files := map[string]Map{}
	if value, exists := global["storage"]; exists {
		section, ok := support.AsMap(value)
		if !ok {
			return fmt.Errorf("storage: config must be an object")
		}
		root := Map{}
		for _, key := range support.Keys(section) {
			switch key {
			case "options":
				fields, ok := support.AsMap(section[key])
				if !ok {
					return fmt.Errorf("storage: options must be an object")
				}
				var err error
				options, err = parseOptions(fields, options)
				if err != nil {
					return err
				}
			case "connections":
				connections, ok := support.AsMap(section[key])
				if !ok {
					return fmt.Errorf("storage: connections must be an object")
				}
				for _, name := range support.Keys(connections) {
					fields, ok := support.AsMap(connections[name])
					if !ok {
						return fmt.Errorf("storage connection %q: config must be an object", name)
					}
					files[name] = support.CloneMap(fields)
				}
			default:
				root[key] = section[key]
			}
		}
		if len(root) > 0 {
			if _, exists := files["default"]; exists {
				return fmt.Errorf("storage: duplicate default connection")
			}
			files["default"] = support.CloneMap(root)
		}
	}
	for _, name := range support.Keys(files) {
		if !validSource(name) {
			return fmt.Errorf("storage: invalid connection name %q", name)
		}
		if _, err := parseConfig(files[name], Config{}); err != nil {
			return fmt.Errorf("storage connection %q: %w", name, err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opened || m.closing {
		return fmt.Errorf("storage: config after open")
	}
	m.options = options
	m.fileConfigs = files
	m.sources = nil
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
		case "proxy", "remote":
			flag, ok := value.(bool)
			if !ok {
				return cfg, fmt.Errorf("%s must be boolean", key)
			}
			if key == "proxy" {
				cfg.Proxy = flag
			} else {
				cfg.Remote = flag
			}
		case "settings":
		default:
			return cfg, fmt.Errorf("unknown config field %q", key)
		}
	}
	cfg.Settings = support.CloneMap(cfg.Settings)
	if value, exists := fields["settings"]; exists {
		settings, ok := support.AsMap(value)
		if !ok {
			return cfg, fmt.Errorf("settings must be an object")
		}
		if cfg.Settings == nil {
			cfg.Settings = Map{}
		}
		for key, value := range settings {
			cfg.Settings[key] = support.CloneValue(value)
		}
	}
	cfg.Type = strings.ToLower(strings.TrimSpace(cfg.Type))
	if cfg.Type == "" {
		cfg.Type = "file"
	}
	if cfg.Weight == 0 {
		cfg.Weight = 1
	}
	if cfg.Weight < -1 {
		return cfg, fmt.Errorf("invalid weight")
	}
	cfg.Prefix = strings.TrimRight(cfg.Prefix, "/")
	if !validRelative(cfg.Prefix, true) {
		return cfg, ErrInvalidPath
	}
	return cfg, nil
}
func parseOptions(fields Map, opt Options) (Options, error) {
	for _, key := range support.Keys(fields) {
		value := fields[key]
		switch key {
		case "key_mode", "temp":
			return opt, fmt.Errorf("storage: %s belongs in the local connection's settings, not options", key)
		case "download", "thumbnail", "preview":
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return opt, fmt.Errorf("storage: invalid %s directory", key)
			}
			switch key {
			case "download":
				opt.Download = text
			case "thumbnail":
				opt.Thumbnail = text
			case "preview":
				opt.Preview = text
			}
		case "cache_ttl":
			duration, err := support.Duration(value)
			if err != nil || duration <= 0 {
				return opt, fmt.Errorf("storage: invalid cache_ttl")
			}
			opt.CacheTTL = duration
		case "max_image_pixels", "max_thumbnail_pixels":
			n, err := support.Integer(value)
			if err != nil || n <= 0 {
				return opt, ErrInvalidOption
			}
			if key == "max_image_pixels" {
				opt.MaxImagePixels = n
			} else {
				opt.MaxThumbnailPixels = n
			}
		default:
			return opt, fmt.Errorf("storage: unknown option %q", key)
		}
	}
	return opt, nil
}
