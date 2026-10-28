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
				content = fmt.Sprintf(`{"storage":{"options":{"download":%q,"thumbnail":%q,"preview":%q,"temp":%q},"sources":{"auto":{"prefix":"tenant","settings":{"root":%q}},"manual":{"weight":-1,"settings":{"root":%q}}}}}`, dir+"/download", dir+"/thumbnail", dir+"/preview", dir+"/temp", dir+"/auto", dir+"/manual")
			case "toml":
				content = fmt.Sprintf("[storage.options]\ndownload='%s/download'\nthumbnail='%s/thumbnail'\npreview='%s/preview'\ntemp='%s/temp'\n[storage.sources.auto]\nprefix='tenant'\n[storage.sources.auto.settings]\nroot='%s/auto'\n[storage.sources.manual]\nweight=-1\n[storage.sources.manual.settings]\nroot='%s/manual'\n", dir, dir, dir, dir, dir, dir)
			case "yaml":
				content = fmt.Sprintf("storage:\n  options:\n    download: '%s/download'\n    thumbnail: '%s/thumbnail'\n    preview: '%s/preview'\n    temp: '%s/temp'\n  sources:\n    auto:\n      prefix: tenant\n      settings:\n        root: '%s/auto'\n    manual:\n      weight: -1\n      settings:\n        root: '%s/manual'\n", dir, dir, dir, dir, dir, dir)
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
func TestConfigAliasesAndValidation(t *testing.T) {
	m := newModule()
	if err := m.Config(Map{"file": Map{"download": "old"}, "storage": Map{"option": Map{"download": "single"}, "options": Map{"download": "plural"}, "type": "file", "setting": Map{"value": "old", "legacy": true}, "settings": Map{"value": "new"}}}); err != nil {
		t.Fatal(err)
	}
	m.RegisterDriver("file", &fileDriver{})
	m.RegisterConfig("default", Config{Prefix: "code", Settings: Map{"code": true}})
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	cfg := m.sources["default"]
	if m.options.Download != "plural" || cfg.Prefix != "code" || cfg.Settings["value"] != "new" || cfg.Settings["legacy"] != true || cfg.Settings["code"] != true {
		t.Fatalf("merge: %+v %+v", m.options, cfg)
	}
	for _, fields := range []Map{{"type": "missing"}, {"prefix": "../x"}, {"settings": "bad"}, {"options": true}, {"options": Map{"max_image_pixels": -1}}, {"weight": -2}, {"sources": Map{"default": Map{}}, "type": "file"}} {
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
