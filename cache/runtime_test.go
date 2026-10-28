package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func TestRuntimeMountsDefaultMemoryWithoutServer(t *testing.T) {
	dubi.Reset()
	t.Cleanup(dubi.Reset)
	t.Setenv("DUBI_CONFIG", "")
	if err := dubi.DefaultRuntime().Run(""); err != nil {
		t.Fatal(err)
	}
	if !Ready() || len(Stats().Instances) != 1 {
		t.Fatalf("default source missing: %+v", Stats())
	}
	if err := Write("user", Map{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if value, err := Read("user"); err != nil || value["name"] != "Ada" {
		t.Fatalf("default cache: %v %v", value, err)
	}
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	if err := Write("closed", Map{}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("runtime end did not close cache: %v", err)
	}
}

func TestRuntimeConfigAndProfileAndCustomCodec(t *testing.T) {
	for extension, content := range map[string]string{
		"json": `{"cache":{"sources":{"a":{"type":"memory","prefix":"file:","settings":{"size":1048576}},"manual":{"weight":-1,"settings":{"size":1048576}}}}}`,
		"toml": "[cache.sources.a]\ntype='memory'\nprefix='file:'\n[cache.sources.a.settings]\nsize=1048576\n[cache.sources.manual]\nweight=-1\n[cache.sources.manual.settings]\nsize=1048576\n",
		"yaml": "cache:\n  sources:\n    a:\n      type: memory\n      prefix: 'file:'\n      settings:\n        size: 1048576\n    manual:\n      weight: -1\n      settings:\n        size: 1048576\n",
	} {
		t.Run(extension, func(t *testing.T) {
			dubi.Reset()
			t.Cleanup(dubi.Reset)
			path := filepath.Join(t.TempDir(), "config."+extension)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DUBI_CONFIG", path)
			dubi.Register("a", Config{Prefix: "code:", Codec: "test-json"})
			dubi.Register("unused", Config{})
			dubi.Register("test-json", dubi.Codec{
				Encode: func(value any) (any, error) { return json.Marshal(value) },
				Decode: func(data any, target any) (any, error) { return target, json.Unmarshal(data.([]byte), target) },
			})
			dubi.Register("cache-only", dubi.Profile{Include: []string{"cache:config:a"}})
			if err := dubi.DefaultRuntime().Run("cache-only"); err != nil {
				t.Fatal(err)
			}
			if len(Stats().Instances) != 2 {
				t.Fatalf("file config/profile selection: %+v", Stats())
			}
			if cfg := module.instances["a"].Config; cfg.Codec != "test-json" || cfg.Prefix != "file:" {
				t.Fatalf("config precedence: %+v", cfg)
			}
			if err := Write("key", Map{"value": true}); err != nil {
				t.Fatal(err)
			}
			if data, err := Read("key"); err != nil || data["value"] != true {
				t.Fatalf("custom codec: %v %v", data, err)
			}
			if err := WriteTo("manual", "manual", Map{"value": true}); err != nil {
				t.Fatal(err)
			}
			if err := dubi.End(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
