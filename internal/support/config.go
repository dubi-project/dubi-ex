package support

import (
	"fmt"
	"strings"

	. "github.com/dubi-project/dubi-go/base"
)

// Sources accepts root single-source fields, explicit sources, and compact
// named sources. Settings are reserved objects, never mistaken for a source.
func Sources(global Map, module string, fields ...string) (map[string]Map, error) {
	out := map[string]Map{}
	value, exists := global[module]
	if !exists {
		return out, nil
	}
	section, ok := AsMap(value)
	if !ok {
		return nil, fmt.Errorf("%s: config must be an object", module)
	}
	allowed := map[string]bool{"type": true, "codec": true, "external": true, "weight": true, "prefix": true, "settings": true, "setting": true}
	for _, key := range fields {
		allowed[key] = true
	}
	add := func(name string, value any) error {
		if name == "" || strings.TrimSpace(name) != name {
			return fmt.Errorf("%s: invalid source name %q", module, name)
		}
		if _, exists := out[name]; exists {
			return fmt.Errorf("%s: duplicate source %q", module, name)
		}
		fields, ok := AsMap(value)
		if !ok {
			return fmt.Errorf("%s source %q: config must be an object", module, name)
		}
		out[name] = CloneMap(fields)
		return nil
	}
	root := Map{}
	for _, key := range Keys(section) {
		value := section[key]
		if allowed[key] {
			root[key] = value
			continue
		}
		if key == "sources" {
			sources, ok := AsMap(value)
			if !ok {
				return nil, fmt.Errorf("%s: sources must be an object", module)
			}
			for _, name := range Keys(sources) {
				if err := add(name, sources[name]); err != nil {
					return nil, err
				}
			}
		} else if err := add(key, value); err != nil {
			return nil, err
		}
	}
	if len(root) > 0 {
		if err := add("default", root); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func MergeSettings(cfg Map, fields Map) (Map, error) {
	out := CloneMap(cfg)
	if out == nil {
		out = Map{}
	}
	for _, key := range []string{"setting", "settings"} {
		if value, exists := fields[key]; exists {
			items, ok := AsMap(value)
			if !ok {
				return nil, fmt.Errorf("%s must be an object", key)
			}
			for name, value := range items {
				out[name] = CloneValue(value)
			}
		}
	}
	return out, nil
}
