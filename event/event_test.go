package event

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func startRuntime(t *testing.T, profile ...string) {
	t.Helper()
	if err := dubi.DefaultRuntime().Run(profile...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dubi.End(); err != nil {
			t.Error(err)
		}
		dubi.Reset()
	})
}
func resetRuntime(t *testing.T) {
	t.Helper()
	dubi.Reset()
	t.Cleanup(dubi.Reset)
	t.Setenv("DUBI_CONFIG", "")
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
		var zero T
		return zero
	}
}

func TestRuntimePublishBroadcastMappingAndMeta(t *testing.T) {
	resetRuntime(t)
	seen := make(chan Map, 4)
	dubi.Register("created", &Event{Alias: []string{"alias"}, Settings: Map{"nested": Map{"value": "default"}}, Args: Vars{"id": {Type: "int", Required: true}}, Action: func(ctx *Context) {
		seen <- Map{"id": ctx.Args["id"], "trace": ctx.TraceId(), "language": ctx.Language(), "setting": ctx.Settings["nested"].(Map)["value"]}
		ctx.Settings["nested"].(Map)["value"] = "mutated"
	}})
	startRuntime(t)
	meta := dubi.NewMeta()
	meta.TraceId("trace-test")
	meta.Language("en")
	if err := PublishWithMeta(meta, "created", Map{"id": "7"}); err != nil {
		t.Fatal(err)
	}
	got := receive(t, seen)
	if got["id"] != int64(7) || got["trace"] != "trace-test" || got["language"] != "en" || got["setting"] != "default" {
		t.Fatalf("event state: %v", got)
	}
	if err := Broadcast("alias", Map{"id": 8}); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, seen); got["id"] != int64(8) || got["setting"] != "default" {
		t.Fatalf("broadcast: %v", got)
	}
	if err := Publish("created", Map{}); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("invalid payload: %v", err)
	}
	if err := Publish("missing"); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid event: %v", err)
	}
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	if Ready() {
		t.Fatal("still ready")
	}
	if err := Publish("created"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("publish after end: %v", err)
	}
}

func TestRuntimeProfileAndNamedSources(t *testing.T) {
	resetRuntime(t)
	path := filepath.Join(t.TempDir(), "event.yaml")
	if err := os.WriteFile(path, []byte("event:\n  sources:\n    primary:\n      type: memory\n      prefix: app\n    manual:\n      weight: -1\n      external: true\n      settings:\n        buffer: 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DUBI_CONFIG", path)
	seen := make(chan string, 4)
	dubi.Register("included", Event{Connect: "*", Action: func(ctx *Context) { seen <- ctx.inst.Name }})
	dubi.Register("excluded", Event{})
	dubi.Register("manual", Config{Prefix: "code:", Settings: Map{"code": true}})
	dubi.Register("selected", dubi.Profile{Include: []string{"event:event:included", "event:config:manual"}})
	startRuntime(t, "selected")
	if len(module.events) != 1 || len(module.instances) != 2 {
		t.Fatalf("selection: events=%v sources=%v", module.events, module.instances)
	}
	if module.instances["manual"].Config.Prefix != "code:" || module.instances["manual"].Settings["code"] != true {
		t.Fatal("file fields overwrote omitted code defaults")
	}
	if err := Publish("included"); err != nil {
		t.Fatal(err)
	}
	if source := receive(t, seen); source != "primary" {
		t.Fatalf("auto source=%s", source)
	}
	if err := PublishTo("manual", "included"); err != nil {
		t.Fatal(err)
	}
	if source := receive(t, seen); source != "manual" {
		t.Fatalf("manual source=%s", source)
	}
}

func TestFiltersHandlersAndStopWaitsWithoutModuleLock(t *testing.T) {
	resetRuntime(t)
	var order []string
	dubi.Register("pipeline", Filter{Serve: func(ctx *Context) { order = append(order, "serve"); ctx.Next() }, Request: func(ctx *Context) { order = append(order, "request"); ctx.Next() }, Execute: func(ctx *Context) { order = append(order, "execute"); ctx.Next() }, Response: func(ctx *Context) { order = append(order, "response") }})
	finished := make(chan []string, 1)
	dubi.Register("done", Handler{Failed: func(ctx *Context) { order = append(order, "failed") }})
	dubi.Register("check", Event{Action: func(ctx *Context) {
		order = append(order, "action")
		ctx.Failed(dubi.Fail)
		finished <- append([]string(nil), order...)
	}})
	startRuntime(t)
	if err := Publish("check"); err != nil {
		t.Fatal(err)
	}
	receive(t, finished)
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	want := []string{"serve", "request", "execute", "action", "failed", "response"}
	if len(order) != len(want) {
		t.Fatalf("pipeline %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("pipeline %v", order)
		}
	}
	// Open again and prove shutdown does not hold m.mutex while waiting on a
	// callback that calls back into Publish.
	dubi.Reset()
	entered := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	dubi.Register("wait", Event{Action: func(ctx *Context) {
		once.Do(func() { close(entered) })
		<-ctx.Context().Done()
		_ = Publish("wait")
		close(stopped)
	}})
	if err := dubi.DefaultRuntime().Run(); err != nil {
		t.Fatal(err)
	}
	if err := Publish("wait"); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	ended := make(chan error, 1)
	go func() { ended <- dubi.End() }()
	receive(t, stopped)
	if err := receive(t, ended); err != nil {
		t.Fatal(err)
	}
}

func TestServeRejectsMalformedEnvelopeAndRecoversPanic(t *testing.T) {
	resetRuntime(t)
	dubi.Register("panic", Event{Action: func(*Context) { panic("boom") }})
	startRuntime(t)
	inst := module.instances["default"]
	if inst.ServeSync("publish.panic", []byte("invalid")) {
		t.Fatal("malformed payload succeeded")
	}
	data, _ := dubi.Marshal("json", msgEnvelope{Name: "different", Payload: Map{}})
	if inst.ServeSync("publish.panic", data) {
		t.Fatal("mismatched envelope succeeded")
	}
	data, _ = dubi.Marshal("json", msgEnvelope{Name: "panic", Payload: Map{}})
	if inst.ServeSync("publish.panic", data) {
		t.Fatal("panic succeeded")
	}
}

func TestBoundedWorkersDrainOnStop(t *testing.T) {
	resetRuntime(t)
	// Leave room for the whole burst: this test exercises draining accepted
	// callbacks, rather than relying on the consumer outrunning the producer.
	dubi.Register("event.defaults", Config{Workers: 2, Queue: 16, DrainTimeout: time.Second})
	seen := make(chan struct{}, 12)
	dubi.Register("work", Event{Action: func(ctx *Context) {
		select {
		case <-time.After(time.Millisecond):
		case <-ctx.Context().Done():
		}
		seen <- struct{}{}
	}})
	startRuntime(t)
	for i := 0; i < 10; i++ {
		if err := Publish("work"); err != nil {
			t.Fatal(err)
		}
	}
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 10 {
		t.Fatalf("accepted work did not drain: %d", len(seen))
	}
}
