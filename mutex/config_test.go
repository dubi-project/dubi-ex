package mutex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func TestRuntimeConfigurationFormatsAndProfile(t *testing.T) {
	for extension, content := range map[string]string{
		"json": `{"mutex":{"sources":{"auto":{"type":"memory","prefix":"app:","expire":"2s","cleanup_interval":"50s","token_grace":"9s","settings":{"file":true}},"manual":{"weight":-1}}}}`,
		"toml": "[mutex.sources.auto]\ntype='memory'\nprefix='app:'\nexpire='2s'\ncleanup_interval='50s'\ntoken_grace='9s'\n[mutex.sources.auto.settings]\nfile=true\n[mutex.sources.manual]\nweight=-1\ncleanup_interval='1m'\n",
		"yaml": "mutex:\n  sources:\n    auto:\n      type: memory\n      prefix: 'app:'\n      expire: 2s\n      cleanup_interval: 50s\n      token_grace: 9s\n      settings:\n        file: true\n    manual:\n      weight: -1\n",
	} {
		t.Run(extension, func(t *testing.T) {
			resetRuntime(t)
			path := filepath.Join(t.TempDir(), "config."+extension)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DUBI_CONFIG", path)
			dubi.Register("auto", &Config{Prefix: "code:", Settings: Map{"code": true}})
			dubi.Register("excluded", Config{})
			dubi.Register("selected", dubi.Profile{Include: []string{"mutex:config:auto"}})
			startRuntime(t, "selected")
			if len(module.instances) != 2 {
				t.Fatalf("sources: %v", module.instances)
			}
			cfg := module.instances["auto"].Config
			if cfg.Prefix != "app:" || cfg.Expire != 2*time.Second || cfg.CleanupInterval != 50*time.Second || cfg.TokenGrace != 9*time.Second || cfg.Settings["code"] != true || cfg.Settings["file"] != true {
				t.Fatalf("config: %+v", cfg)
			}
			for i := 0; i < 20; i++ {
				if source := module.ring.Locate(fmt.Sprint(i)); source != "auto" {
					t.Fatalf("automatic source: %s", source)
				}
			}
			lock, err := Lock("routed")
			if err != nil {
				t.Fatal(err)
			}
			if lock.conn != "auto" {
				t.Fatal("wrong lock source")
			}
			other, err := LockOn("manual", "routed")
			if err != nil {
				t.Fatal(err)
			}
			if other.conn != "manual" {
				t.Fatal("explicit source ignored")
			}
			if err := lock.Unlock(); err != nil {
				t.Fatal(err)
			}
			if !LockedOn("manual", "routed") {
				t.Fatal("sources share backend locks")
			}
			if err := other.Unlock(); err != nil {
				t.Fatal(err)
			}
			if _, err := StatsFrom("missing"); !errors.Is(err, ErrInvalidConnection) {
				t.Fatalf("stats source: %v", err)
			}
			if _, err := CapabilityFrom("missing"); !errors.Is(err, ErrInvalidConnection) {
				t.Fatalf("capability source: %v", err)
			}
		})
	}
}

func TestConfigurationAliasesAndValidation(t *testing.T) {
	m := newModule()
	m.RegisterDriver("memory", &defaultDriver{})
	if err := m.Config(Map{"mutex": Map{"primary": Map{"expire": 1.5, "setting": Map{"choice": "old", "old": true}, "settings": Map{"choice": "new"}}}}); err != nil {
		t.Fatal(err)
	}
	m.RegisterConfig("primary", Config{Settings: Map{"code": true}})
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	cfg := m.sources["primary"]
	if cfg.Expire != 1500*time.Millisecond || cfg.Settings["choice"] != "new" || cfg.Settings["old"] != true || cfg.Settings["code"] != true {
		t.Fatalf("config: %+v", cfg)
	}
	for _, fields := range []Map{{"sources": "bad"}, {"settings": false}, {"expire": "bad"}, {"expire": -1}, {"weight": -2}, {"weight": 1.5}, {"type": "missing"}, {"codec": "json"}, {"sources": Map{"default": Map{}}, "type": "memory"}} {
		m := newModule()
		m.RegisterDriver("memory", &defaultDriver{})
		err := m.Config(Map{"mutex": fields})
		if err == nil {
			err = m.Setup(nil)
		}
		if err == nil {
			t.Fatalf("accepted config: %v", fields)
		}
	}
}

func TestNegativeLeaseAndManualOnlySources(t *testing.T) {
	resetRuntime(t)
	if _, err := Lock("early"); !errors.Is(err, ErrNotReady) {
		t.Fatalf("early lock: %v", err)
	}
	dubi.Register("manual", Configs{"only": {Weight: -1, CleanupInterval: time.Minute}})
	startRuntime(t)
	if module.cleanupInterval != time.Minute {
		t.Fatalf("cleanup interval %v", module.cleanupInterval)
	}
	if _, err := Lock("no-auto"); !errors.Is(err, ErrInvalidConnection) {
		t.Fatalf("manual-only routing: %v", err)
	}
	if _, err := LockOn("only", "negative", -time.Second); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("negative lease: %v", err)
	}
	lk, err := LockOn("only", "valid")
	if err != nil {
		t.Fatal(err)
	}
	if err := lk.Refresh(-time.Second); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("negative refresh: %v", err)
	}
	if err := lk.Unlock(); err != nil {
		t.Fatal(err)
	}
}
