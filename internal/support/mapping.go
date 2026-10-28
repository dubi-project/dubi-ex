package support

import (
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

// Mapping adapts the component-level Nullable flag to dubi-go's per-field
// definitions. Partial payloads omit missing fields and do not inject defaults.
func Mapping(meta *dubi.Meta, fields Vars, value Map, nullable bool) (Map, Res) {
	if nullable {
		fields = optionalFields(fields)
	}
	if meta != nil {
		return meta.Mapping(fields, value)
	}
	return dubi.Mapping(fields, value)
}

func optionalFields(fields Vars) Vars {
	out := fields.Clone()
	for name, field := range out {
		field.Required = false
		field.Nullable = true
		field.Default = nil
		field.Children = optionalFields(field.Children)
		out[name] = field
	}
	return out
}
