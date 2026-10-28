package cache

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

// Config receives configuration already loaded by dubi-go. It never reads
// configuration files itself. File fields override code defaults field by field.
func (m *Module) Config(global Map) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.opened {
		return fmt.Errorf("cache: configure after open")
	}
	files := make(map[string]Map)
	if value, exists := global["cache"]; exists {
		section, ok := asMap(value)
		if !ok {
			return fmt.Errorf("cache: config must be an object")
		}
		root := Map{}
		for _, key := range sortedKeys(section) {
			value := section[key]
			switch key {
			case "sources":
				sources, ok := asMap(value)
				if !ok {
					return fmt.Errorf("cache: sources must be an object")
				}
				for _, name := range sortedKeys(sources) {
					config, ok := asMap(sources[name])
					if !ok {
						return fmt.Errorf("cache source %q: config must be an object", name)
					}
					if err := addFileConfig(files, name, config); err != nil {
						return err
					}
				}
			case "type", "weight", "prefix", "codec", "expire", "ttl", "allow_clear_all", "clear_all", "setting", "settings":
				root[key] = value
			default:
				// Preserve infrago's compact [cache.name] configuration form.
				config, ok := asMap(value)
				if !ok {
					return fmt.Errorf("cache: unknown config field %q", key)
				}
				if err := addFileConfig(files, key, config); err != nil {
					return err
				}
			}
		}
		if len(root) > 0 {
			if err := addFileConfig(files, defaultName, root); err != nil {
				return err
			}
		}
	}
	m.fileConfigs = files
	// Runtime may retry setup after another module fails. No source has been
	// opened yet, so discard the prepared routing table and resolve again.
	m.prepared, m.hashring, m.sources = false, nil, nil
	return nil
}

func addFileConfig(files map[string]Map, name string, fields Map) error {
	if name == "" || name != strings.TrimSpace(name) {
		return fmt.Errorf("cache: invalid source name %q", name)
	}
	if _, exists := files[name]; exists {
		return fmt.Errorf("cache: duplicate source %q", name)
	}
	if _, err := parseConfig(fields, Config{}); err != nil {
		return fmt.Errorf("cache source %q: %w", name, err)
	}
	files[name] = cloneMap(fields)
	return nil
}

func parseConfig(fields Map, cfg Config) (Config, error) {
	for _, key := range sortedKeys(fields) {
		value := fields[key]
		switch key {
		case "type", "prefix", "codec":
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("%s must be a string", key)
			}
			switch key {
			case "type":
				cfg.Type = text
			case "prefix":
				cfg.Prefix = text
			case "codec":
				cfg.Codec = text
			}
		case "weight":
			number, err := integer(value)
			if err != nil || int64(int(number)) != number || number < -1 {
				return cfg, fmt.Errorf("invalid weight %v", value)
			}
			cfg.Weight = int(number)
		case "expire", "ttl":
			duration, err := parseDuration(value)
			if err != nil {
				return cfg, fmt.Errorf("invalid %s: %w", key, err)
			}
			cfg.Expire = duration
		case "allow_clear_all", "clear_all":
			flag, ok := value.(bool)
			if !ok {
				return cfg, fmt.Errorf("%s must be a boolean", key)
			}
			cfg.AllowClearAll = flag
		case "setting", "settings":
			setting, ok := asMap(value)
			if !ok {
				return cfg, fmt.Errorf("%s must be an object", key)
			}
			cfg.Settings = cloneMap(cfg.Settings)
			if cfg.Settings == nil {
				cfg.Settings = Map{}
			}
			for key, value := range cloneMap(setting) {
				cfg.Settings[key] = value
			}
		default:
			return cfg, fmt.Errorf("unknown config field %q", key)
		}
	}
	return normalizeConfig(cfg)
}

func normalizeConfig(cfg Config) (Config, error) {
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
		return cfg, fmt.Errorf("weight must be -1 or non-negative")
	}
	cfg.Settings = cloneMap(cfg.Settings)
	return cfg, nil
}

func integer(value any) (int64, error) {
	if text, ok := value.(string); ok {
		return strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	}
	if number, ok := value.(json.Number); ok {
		return number.Int64()
	}
	v := reflect.ValueOf(value)
	if v.IsValid() {
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return v.Int(), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if n := v.Uint(); n <= math.MaxInt64 {
				return int64(n), nil
			}
		case reflect.Float32, reflect.Float64:
			if n := v.Float(); n >= math.MinInt64 && n < float64(math.MaxInt64) && math.Trunc(n) == n {
				return int64(n), nil
			}
		}
	}
	return 0, fmt.Errorf("not an integer: %v", value)
}

func parseDuration(value any) (time.Duration, error) {
	if duration, ok := value.(time.Duration); ok {
		return duration, nil
	}
	if text, ok := value.(string); ok {
		return time.ParseDuration(strings.TrimSpace(text))
	}
	// Numeric values from JSON/TOML/YAML are seconds, including fractional seconds.
	if number, ok := value.(json.Number); ok {
		v, err := number.Float64()
		if err != nil {
			return 0, err
		}
		value = v
	}
	v := reflect.ValueOf(value)
	var seconds float64
	if !v.IsValid() {
		return 0, fmt.Errorf("invalid duration %v", value)
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		seconds = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		seconds = float64(v.Uint())
	case reflect.Float32, reflect.Float64:
		seconds = v.Float()
	default:
		return 0, fmt.Errorf("invalid duration %v", value)
	}
	nanos := seconds * float64(time.Second)
	if math.IsNaN(nanos) || math.IsInf(nanos, 0) || nanos >= float64(math.MaxInt64) || nanos < math.MinInt64 {
		return 0, fmt.Errorf("duration out of range")
	}
	return time.Duration(nanos), nil
}

func asMap(value any) (Map, bool) {
	switch item := value.(type) {
	case Map:
		return item, true
	case map[string]any:
		return Map(item), true
	default:
		return nil, false
	}
}

func cloneMap(value Map) Map {
	if value == nil {
		return nil
	}
	out := make(Map, len(value))
	for key, value := range value {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	if object, ok := asMap(value); ok {
		return cloneMap(object)
	}
	switch items := value.(type) {
	case []any:
		copy := make([]any, len(items))
		for i, item := range items {
			copy[i] = cloneValue(item)
		}
		return copy
	case []string:
		return append([]string(nil), items...)
	case []Map:
		copy := make([]Map, len(items))
		for i, item := range items {
			copy[i] = cloneMap(item)
		}
		return copy
	case List:
		copy := make(List, len(items))
		for i, item := range items {
			copy[i] = cloneValue(item)
		}
		return copy
	case []byte:
		return append([]byte(nil), items...)
	default:
		return value
	}
}
