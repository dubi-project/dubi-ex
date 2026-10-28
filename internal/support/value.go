package support

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

func Integer(value any) (int64, error) {
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

func Duration(value any) (time.Duration, error) {
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

func AsMap(value any) (Map, bool) {
	switch item := value.(type) {
	case Map:
		return item, true
	case map[string]any:
		return Map(item), true
	default:
		return nil, false
	}
}

func CloneMap(value Map) Map {
	if value == nil {
		return nil
	}
	out := make(Map, len(value))
	for key, value := range value {
		out[key] = CloneValue(value)
	}
	return out
}

func CloneValue(value any) any {
	if object, ok := AsMap(value); ok {
		return CloneMap(object)
	}
	switch items := value.(type) {
	case []any:
		copy := make([]any, len(items))
		for i, item := range items {
			copy[i] = CloneValue(item)
		}
		return copy
	case []string:
		return append([]string(nil), items...)
	case []Map:
		copy := make([]Map, len(items))
		for i, item := range items {
			copy[i] = CloneMap(item)
		}
		return copy
	case List:
		copy := make(List, len(items))
		for i, item := range items {
			copy[i] = CloneValue(item)
		}
		return copy
	case []byte:
		return append([]byte(nil), items...)
	default:
		return value
	}
}

func Keys[V any](items map[string]V) []string {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
