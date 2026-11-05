package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func TestConfigFormatsSourcesAndProfile(t *testing.T) {
	for _, format := range []string{"json", "toml", "yaml"} {
		t.Run(format, func(t *testing.T) {
			dubi.Reset()
			t.Cleanup(dubi.Reset)
			dir := filepath.ToSlash(t.TempDir())
			config := filepath.Join(dir, "config."+format)
			var content string
			switch format {
			case "json":
				content = fmt.Sprintf(`{"storage":{"options":{"download":%q,"thumbnail":%q,"preview":%q},"connections":{"auto":{"prefix":"tenant","settings":{"root":%q,"temp":%q}},"manual":{"weight":-1,"settings":{"root":%q,"temp":%q,"key_mode":"random"}}}}}`, dir+"/download", dir+"/thumbnail", dir+"/preview", dir+"/auto", dir+"/temp-auto", dir+"/manual", dir+"/temp-manual")
			case "toml":
				content = fmt.Sprintf(`[storage.options]
download='%[1]s/download'
thumbnail='%[1]s/thumbnail'
preview='%[1]s/preview'
[storage.connections.auto]
prefix='tenant'
[storage.connections.auto.settings]
root='%[1]s/auto'
temp='%[1]s/temp-auto'
[storage.connections.manual]
weight=-1
[storage.connections.manual.settings]
root='%[1]s/manual'
temp='%[1]s/temp-manual'
key_mode='random'
`, dir)
			case "yaml":
				content = fmt.Sprintf(`storage:
  options:
    download: '%[1]s/download'
    thumbnail: '%[1]s/thumbnail'
    preview: '%[1]s/preview'
  connections:
    auto:
      prefix: tenant
      settings:
        root: '%[1]s/auto'
        temp: '%[1]s/temp-auto'
    manual:
      weight: -1
      settings:
        root: '%[1]s/manual'
        temp: '%[1]s/temp-manual'
        key_mode: random
`, dir)
			}
			if err := os.WriteFile(config, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DUBI_CONFIG", config)
			dubi.Register("auto", &Config{Settings: Map{"base_url": "https://files.example.test/assets"}})
			dubi.Register("ignored", Config{})
			dubi.Register("selected", dubi.Profile{Include: []string{"storage:config:auto"}})
			startRuntime(t, "selected")
			if len(module.instances) != 2 {
				t.Fatalf("profile/sources: %v", module.instances)
			}
			auto := module.instances["auto"].conn.(*fileConnection)
			manualConn := module.instances["manual"].conn.(*fileConnection)
			if auto.keyMode != HashKey || manualConn.keyMode != RandomKey || auto.temp == manualConn.temp {
				t.Fatalf("connection settings not isolated: %+v %+v", auto, manualConn)
			}
			hashed, err := UploadReaderTo("auto", strings.NewReader("abc"))
			if err != nil || len(hashed.Key()) != 27 {
				t.Fatalf("hash connection: %+v %v", hashed, err)
			}
			random, err := UploadReaderTo("manual", strings.NewReader("abc"))
			if err != nil || len(random.Key()) != 32 {
				t.Fatalf("random connection: %+v %v", random, err)
			}
			if _, err := os.Stat(auto.temp); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(manualConn.temp); !os.IsNotExist(err) {
				t.Fatalf("random connection unexpectedly used staging: %v", err)
			}
			first, err := UploadReader(strings.NewReader("one"), UploadOption{Key: "same", Prefix: "business", Name: "file.txt"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := UploadReader(strings.NewReader("two"), UploadOption{Key: "same", Prefix: "business", Name: "file.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if first.Base() != "auto" || second.Base() != "auto" || first.Prefix() != "tenant/business" {
				t.Fatalf("routing: %+v %+v", first, second)
			}
			manual, err := UploadReaderTo("manual", strings.NewReader("manual"), UploadOption{Key: "same", Name: "file.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if manual.Base() != "manual" || read(t, manual.Code()) != "manual" || read(t, first.Code()) != "two" {
				t.Fatal("source isolation")
			}
			url, err := Browse(first.Code(), BrowseOption{Params: Map{"download": "yes"}})
			if err != nil || url != "https://files.example.test/assets/tenant/business/same.txt?download=yes" {
				t.Fatalf("URL: %s %v", url, err)
			}
		})
	}
}
func TestConfigDefaultsAndValidation(t *testing.T) {
	m := newModule()
	if err := m.Config(Map{"storage": Map{"options": Map{"download": "custom"}, "type": "file", "settings": Map{"root": "configured"}}}); err != nil {
		t.Fatal(err)
	}
	m.RegisterDriver("file", &fileDriver{})
	m.RegisterConfig("default", Config{Prefix: "code", Settings: Map{"root": "code", "key_mode": "random"}})
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	cfg := m.sources["default"]
	if m.options.Download != "custom" || cfg.Prefix != "code" || cfg.Settings["root"] != "configured" || cfg.Settings["key_mode"] != "random" {
		t.Fatalf("merge: %+v %+v", m.options, cfg)
	}
	for _, fields := range []Map{{"type": "missing"}, {"prefix": "../x"}, {"settings": "bad"}, {"options": true}, {"options": Map{"max_image_pixels": -1}}, {"options": Map{"key_mode": "unknown"}}, {"options": Map{"key_mode": 1}}, {"weight": -2}, {"sources": Map{"default": Map{}}, "type": "file"}} {
		m := newModule()
		m.RegisterDriver("file", &fileDriver{})
		err := m.Config(Map{"storage": fields})
		if err == nil {
			err = m.Setup(nil)
		}
		if err == nil {
			t.Fatalf("accepted: %v", fields)
		}
	}
}
func TestMimetypeAndExternalChanges(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	file, err := UploadReader(strings.NewReader("data"), UploadOption{Name: "temporary.tmp", Mimetype: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if file.Type() != "jpg" {
		t.Fatalf("noncanonical media extension: %s", file.Type())
	}
	cache, err := Download(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	local, err := Local(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("external update"), 0600); err != nil {
		t.Fatal(err)
	}
	fresh, err := Download(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(fresh)
	if fresh == cache || string(data) != "external update" {
		t.Fatal("backend version was not checked")
	}
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if _, err := Download(file.Code()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external deletion ignored: %v", err)
	}
	encoded, err := json.Marshal(file)
	if err != nil || len(encoded) == 0 {
		t.Fatal(err)
	}
}
