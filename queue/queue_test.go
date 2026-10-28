package queue

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func resetRuntime(t *testing.T) {
	t.Helper()
	dubi.Reset()
	t.Cleanup(dubi.Reset)
	t.Setenv("DUBI_CONFIG", "")
}
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
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for queue")
		var zero T
		return zero
	}
}

func TestRuntimeRetryDelayDeadLetterAndMetadata(t *testing.T) {
	resetRuntime(t)
	attempts := make(chan Map, 5)
	dead := make(chan Map, 1)
	dubi.Register("work", &Queue{Alias: []string{"alias"}, Retry: Retry{time.Millisecond, time.Millisecond}, Settings: Map{"dead": "dead", "nested": Map{"value": "original"}}, Args: Vars{"id": {Type: "int", Required: true}}, Action: func(ctx *Context) {
		attempts <- Map{"attempt": ctx.Attempts(), "final": ctx.Final(), "id": ctx.Args["id"], "trace": ctx.TraceId(), "setting": ctx.Settings["nested"].(Map)["value"]}
		ctx.Settings["nested"].(Map)["value"] = "changed"
		ctx.Result(dubi.Fail)
		ctx.Retry()
	}})
	dubi.Register("dead", Queue{Action: func(ctx *Context) { dead <- ctx.Value; ctx.Finish() }})
	startRuntime(t)
	meta := dubi.NewMeta()
	meta.TraceId("queue-trace")
	if err := PublishWithMeta(meta, "alias", Map{"id": "9"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		got := receive(t, attempts)
		if got["attempt"] != i || got["final"] != (i == 3) || got["id"] != int64(9) || got["trace"] != "queue-trace" || got["setting"] != "original" {
			t.Fatalf("attempt %d: %v", i, got)
		}
	}
	if got := receive(t, dead); got["id"] != float64(9) {
		t.Fatalf("dead payload %v", got)
	}
	if err := Publish("work", Map{}); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("invalid payload %v", err)
	}
	if err := Publish("missing"); !errors.Is(err, ErrInvalidQueue) {
		t.Fatalf("missing queue %v", err)
	}
}

func TestDeferredPublishCancellationAndShutdown(t *testing.T) {
	resetRuntime(t)
	seen := make(chan time.Time, 4)
	dubi.Register("job", Queue{Action: func(ctx *Context) { seen <- time.Now() }})
	startRuntime(t)
	before := time.Now()
	if err := DeferredPublish("job", Map{}, 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, seen); got.Sub(before) < 30*time.Millisecond {
		t.Fatalf("delivered early: %v", got.Sub(before))
	}
	if err := DeferredPublish("job", Map{}, time.Hour); err != nil {
		t.Fatal(err)
	}
	inst := module.instances["default"]
	conn := inst.conn.(*defaultConnection)
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	if len(conn.timers) != 0 {
		t.Fatal("pending timers survived shutdown")
	}
	if err := Publish("job"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("closed publish %v", err)
	}
}

func TestNamedSourcesProfileAndThreadConcurrency(t *testing.T) {
	resetRuntime(t)
	path := filepath.Join(t.TempDir(), "queue.toml")
	if err := os.WriteFile(path, []byte("[queue.sources.auto]\ntype='memory'\nprefix='app:'\n[queue.sources.manual]\nweight=-1\nexternal=true\n[queue.sources.manual.settings]\nbuffer=64\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DUBI_CONFIG", path)
	seen := make(chan string, 64)
	dubi.Register("job", Queue{Connect: "*", Thread: 4, Action: func(ctx *Context) { seen <- ctx.inst.Name }})
	dubi.Register("other", Queue{})
	dubi.Register("worker", dubi.Profile{Include: []string{"queue:queue:job"}})
	startRuntime(t, "worker")
	if len(module.queues) != 1 || len(module.instances) != 2 {
		t.Fatal("profile/source load mismatch")
	}
	for i := 0; i < 32; i++ {
		if err := Publish("job", Map{"id": i}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 32; i++ {
		if got := receive(t, seen); got != "auto" {
			t.Fatalf("auto source %q", got)
		}
	}
	if err := PublishTo("manual", "job"); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, seen); got != "manual" {
		t.Fatalf("manual source %q", got)
	}
	if conn := module.instances["auto"].conn.(*defaultConnection); conn.workers["app:job"] != 4 {
		t.Fatal("thread count not applied")
	}
}

func TestFullQueueAndStopDoesNotHoldModuleLock(t *testing.T) {
	resetRuntime(t)
	entered := make(chan struct{})
	var calls atomic.Int32
	dubi.Register("job", Queue{Settings: Map{"buffer": 1}, Action: func(ctx *Context) { calls.Add(1); close(entered); <-ctx.Done(); _ = Publish("job") }})
	startRuntime(t)
	if err := Publish("job"); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	if err := Publish("job"); err != nil {
		t.Fatal(err)
	}
	if err := Publish("job"); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("full queue %v", err)
	}
	ended := make(chan error, 1)
	go func() { ended <- dubi.End() }()
	if err := receive(t, ended); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("executed after cancellation: %d", calls.Load())
	}
}

func TestPanicRetryAndMalformedEnvelope(t *testing.T) {
	resetRuntime(t)
	dubi.Register("panic", Queue{Retry: Retry{time.Millisecond}, Action: func(*Context) { panic("boom") }})
	startRuntime(t)
	inst := module.instances["default"]
	data, _ := dubi.Marshal("json", msgEnvelope{Name: "panic", Payload: Map{}})
	if res := inst.Serve(Request{Name: "panic", Data: data, Attempt: 1}); !res.Retry {
		t.Fatal("panic was not retried")
	}
	if res := inst.Serve(Request{Name: "panic", Data: data, Attempt: 2}); res.Retry {
		t.Fatal("final panic was retried")
	}
	if res := inst.Serve(Request{Name: "panic", Data: []byte("invalid"), Attempt: 1}); res.Retry {
		t.Fatal("malformed payload was retried")
	}
	data, _ = dubi.Marshal("json", msgEnvelope{Name: "different", Payload: Map{}})
	if res := inst.Serve(Request{Name: "panic", Data: data, Attempt: 1}); res.Retry {
		t.Fatal("mismatched envelope executed handler")
	}
}
