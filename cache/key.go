package cache

import (
	"fmt"
	"strings"
)

func Key(parts ...any) string {
	return KeyWith(":", parts...)
}

func KeyWith(sep string, parts ...any) string {
	if len(parts) == 0 {
		return ""
	}
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			items = append(items, "")
			continue
		}
		items = append(items, fmt.Sprint(part))
	}
	return strings.Join(items, sep)
}
