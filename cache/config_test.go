package cache

import (
	"math"
	"testing"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

func TestConfigFormsAndFieldPrecedence(t *testing.T) {
	for _, structured := range []bool{false, true} {
		t.Run(map[bool]string{false: "infrago-style", true: "sources"}[structured], func(t *testing.T) {
			m := &Module{}
			m.RegisterDriver("memory", &memoryDriver{})
			hot := map[string]any{"type": "memory", "prefix": "hot:", "weight": 3, "ttl": 0.5, "setting": map[string]any{"size": 1 << 20}}
			section := Map{"prefix": "root:", "expire": "2s", "setting": Map{"size": 1 << 20}}
			if structured {
				section["sources"] = map[string]any{"hot": hot}
			} else {
				section["hot"] = hot
			}
			if err := m.Config(Map{"cache": section}); err != nil {
				t.Fatal(err)
			}
			// Runtime delivers file config first, then registry defaults. Explicit
			// zero/false file values still win over non-zero code defaults.
			m.Register("default", &Config{Prefix: "code:", Codec: "json", Weight: 2, Settings: Map{"code": true}})
			if err := m.Open(nil); err != nil {
				t.Fatal(err)
			}
			defer m.Close(nil)
			cfg := m.instances["default"].Config
			if cfg.Prefix != "root:" || cfg.Expire != 2*time.Second || cfg.Weight != 2 || cfg.Codec != "json" || cfg.Settings["code"] != true {
				t.Fatalf("precedence: %+v", cfg)
			}
			if cfg := m.instances["hot"].Config; cfg.Expire != 500*time.Millisecond || cfg.Weight != 3 || cfg.Prefix != "hot:" {
				t.Fatalf("hot source: %+v", cfg)
			}
		})
	}
	m := &Module{}
	m.RegisterDriver("memory", &memoryDriver{})
	if err := m.Config(Map{"cache": Map{"weight": 0, "expire": 0, "allow_clear_all": false}}); err != nil {
		t.Fatal(err)
	}
	m.RegisterConfig("default", Config{Weight: 5, Expire: time.Hour, AllowClearAll: true})
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	if cfg := m.sources["default"]; cfg.Weight != 1 || cfg.Expire != 0 || cfg.AllowClearAll {
		t.Fatalf("explicit zeros lost: %+v", cfg)
	}
}

func TestConfigCopiesSettingsAndCanReprepare(t *testing.T) {
	m := &Module{}
	m.RegisterDriver("memory", &memoryDriver{})
	settings := Map{"size": 1 << 20, "nested": Map{"secret": "original"}}
	file := Map{"cache": Map{"prefix": "one:", "setting": settings}}
	if err := m.Config(file); err != nil {
		t.Fatal(err)
	}
	settings["nested"].(Map)["secret"] = "changed"
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	if m.sources["default"].Settings["nested"].(Map)["secret"] != "original" {
		t.Fatal("caller mutated driver config")
	}
	if err := m.Config(Map{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	if m.sources["default"].Prefix != "" {
		t.Fatal("removed file settings survived startup retry")
	}
}

func TestSettingsAliasesAndDriverConfiguration(t *testing.T) {
	for _, spelling := range []string{"setting", "settings"} {
		t.Run(spelling, func(t *testing.T) {
			m := &Module{}
			m.RegisterDriver("memory", &memoryDriver{})
			if err := m.Config(Map{"cache": Map{spelling: Map{"size": 1 << 20}}}); err != nil {
				t.Fatal(err)
			}
			m.RegisterConfig("default", Config{Settings: Map{"code": true}})
			if err := m.Open(nil); err != nil {
				t.Fatal(err)
			}
			defer m.Close(nil)
			inst := m.instances[defaultName]
			if len(m.instances) != 1 || inst.Settings["code"] != true || inst.Settings["size"] != 1<<20 || inst.connect.(*memoryConnection).size != 1<<20 {
				t.Fatalf("settings not delivered to memory driver: %+v", inst)
			}
		})
	}
	m := &Module{}
	m.RegisterDriver("memory", &memoryDriver{})
	if err := m.Config(Map{"cache": Map{
		"setting":  Map{"size": 1 << 19, "legacy": true},
		"settings": Map{"size": 1 << 20},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	defer m.Close(nil)
	if settings := m.instances[defaultName].Settings; settings["size"] != 1<<20 || settings["legacy"] != true {
		t.Fatalf("plural settings precedence: %v", settings)
	}
	if err := (&Module{}).Config(Map{"cache": Map{"settings": false}}); err == nil {
		t.Fatal("non-object settings accepted")
	}
}

func TestInvalidConfigurationFailsBeforeOpening(t *testing.T) {
	inputs := []any{
		true,
		Map{"sources": "invalid"},
		Map{"sources": Map{"a": 42}},
		Map{"sources": Map{"": Map{}}},
		Map{"sources": Map{"default": Map{}}, "type": "memory"},
		Map{"weight": 1.5}, Map{"weight": -2}, Map{"weight": math.Inf(1)},
		Map{"expire": "bad"}, Map{"expire": math.Inf(1)},
		Map{"type": 1}, Map{"codec": false}, Map{"prefix": 1}, Map{"setting": "bad"}, Map{"allow_clear_all": "yes"},
		Map{"unknown": true}, Map{"sources": Map{"a": Map{"unknown": true}}},
	}
	for _, input := range inputs {
		m := &Module{}
		if err := m.Config(Map{"cache": input}); err == nil {
			t.Errorf("accepted invalid configuration: %#v", input)
		}
	}
	for _, cfg := range []Config{{Type: "unknown"}, {Weight: -2}, {Weight: maxRingPoints}, {Settings: Map{"size": 0}}, {Settings: Map{"size": "bad"}}} {
		m := &Module{}
		m.RegisterDriver("memory", &memoryDriver{})
		m.RegisterConfig("default", cfg)
		if err := m.Open(nil); err == nil {
			_ = m.Close(nil)
			t.Errorf("accepted invalid source: %+v", cfg)
		}
		if m.Ready() || len(m.instances) != 0 {
			t.Fatal("failed open published a connection")
		}
	}
}
