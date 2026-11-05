package storage

import (
	"strings"
	"testing"

	. "github.com/dubi-project/dubi-go/base"
)

func TestConnectionsRootDoesNotSupplyDefaults(t *testing.T) {
	m := newModule()
	m.RegisterDriver("file", &fileDriver{})
	if err := m.Config(Map{"storage": Map{
		"type": "file", "prefix": "root-only", "settings": Map{"key_mode": "random", "root": "root-only"},
		"options": Map{"cache_ttl": "1m"},
		"connections": Map{
			"archive": Map{"type": "file"},
			"options": Map{"type": "file"}, // Reserved words are safe inside connections.
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	if len(m.sources) != 3 || m.sources["default"].Prefix != "root-only" {
		t.Fatalf("root connection: %+v", m.sources)
	}
	for _, name := range []string{"archive", "options"} {
		cfg := m.sources[name]
		if cfg.Prefix != "" || len(cfg.Settings) != 0 {
			t.Fatalf("%s inherited root defaults: %+v", name, cfg)
		}
	}
}

func TestConnectionsValidation(t *testing.T) {
	for _, section := range []Map{
		{"connections": Map{"archive": Map{"weight": -1}}},
	} {
		m := newModule()
		if err := m.Config(Map{"storage": section}); err != nil {
			t.Fatal(err)
		}
		if len(m.fileConfigs) != 1 || m.fileConfigs["archive"]["weight"] != -1 {
			t.Fatalf("connection form: %+v", m.fileConfigs)
		}
	}
	for _, section := range []Map{
		{"sources": Map{"archive": Map{"weight": -1}}},
		{"archive": Map{"weight": -1}},
		{"option": Map{"download": "old"}},
		{"setting": Map{"root": "old"}},
		{"options": Map{"thumb": "old"}},
		{"connections": Map{"archive": Map{"setting": Map{"root": "old"}}}},
		{"connections": true},
		{"connections": Map{"archive": false}},
		{"connections": Map{"../bad": Map{}}},
		{"connections": Map{"default": Map{}}, "type": "file"},
		{"connections": Map{"a": Map{}}, "sources": Map{"a": Map{}}},
		{"connections": Map{"a": Map{}}, "a": Map{}},
	} {
		if err := newModule().Config(Map{"storage": section}); err == nil {
			t.Fatalf("accepted invalid connections: %+v", section)
		}
	}
	for key, value := range (Map{"key_mode": "hash", "temp": "staging"}) {
		err := newModule().Config(Map{"storage": Map{"options": Map{key: value}}})
		if err == nil || !strings.Contains(err.Error(), "settings") {
			t.Fatalf("missing migration hint for %s: %v", key, err)
		}
	}
}

func TestLocalDriverSettingsValidation(t *testing.T) {
	for _, settings := range []Map{
		{"key_mode": "unknown"}, {"key_mode": 1}, {"key_mode": ""},
		{"temp": true}, {"temp": ""}, {"temp": "  "},
		{"storage": "old"}, {"url": "https://files.example.test"},
	} {
		if _, err := (&fileDriver{}).Connect(&Instance{Name: "default", Settings: settings}); err == nil {
			t.Fatalf("accepted invalid local settings: %+v", settings)
		}
	}
}

func TestGlobalFileSectionIsNotStorageConfig(t *testing.T) {
	m := newModule()
	if err := m.Config(Map{"file": Map{"download": "old", "temp": "old"}}); err != nil {
		t.Fatal(err)
	}
	if m.options != defaultOptions() || len(m.fileConfigs) != 0 {
		t.Fatal("storage consumed unrelated global file section")
	}
}
