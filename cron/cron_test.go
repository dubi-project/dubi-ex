package cron

import (
	"os"
	"path/filepath"
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
		t.Fatal("timed out waiting for cron")
		var zero T
		return zero
	}
}

func TestSchedulerInvokeHistoryAndProfile(t *testing.T) {
	resetRuntime(t)
	seen := make(chan Map, 8)
	dubi.Register("tick", dubi.Method{Action: func(ctx *dubi.Context) Map { seen <- ctx.Args; return nil }})
	dubi.Register("clock", &Job{Target: "tick", Schedule: "@every 1s", Schedules: []string{"@every 1s"}, Payload: Map{"id": "7"}})
	dubi.Register("excluded", Job{Target: "tick", Schedule: "@every 1s"})
	dubi.Register("worker", dubi.Profile{Include: []string{"core:method:tick", "cron:job:clock"}})
	startRuntime(t, "worker")
	if !Ready() || len(ListJobs()) != 1 || len(module.schedules["clock"]) != 1 {
		t.Fatal("job/profile/schedule loading failed")
	}
	if got := receive(t, seen); got["id"] != "7" {
		t.Fatalf("job payload %v", got)
	}
	if err := Disable("clock"); err != nil {
		t.Fatal(err)
	}
	// Stop waits for in-flight executions, including their history writes.
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	if Ready() {
		t.Fatal("scheduler still running")
	}
	job := ListJobs()["clock"]
	if !job.Disabled {
		t.Fatal("disable not saved")
	}
}

func TestRuntimeConfigAndValidation(t *testing.T) {
	for extension, content := range map[string]string{
		"json": `{"cron":{"type":"memory","tick":"25ms","sync":2,"lock_ttl":"3s","settings":{"file":true}}}`,
		"toml": "[cron]\ntype='memory'\ntick='25ms'\nsync=2\nlock_ttl='3s'\n[cron.settings]\nfile=true\n",
		"yaml": "cron:\n  type: memory\n  tick: 25ms\n  sync: 2\n  lock_ttl: 3s\n  settings:\n    file: true\n",
	} {
		t.Run(extension, func(t *testing.T) {
			resetRuntime(t)
			path := filepath.Join(t.TempDir(), "config."+extension)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DUBI_CONFIG", path)
			dubi.Register("cron.defaults", Config{Tick: time.Second, Settings: Map{"code": true}})
			startRuntime(t)
			if cfg := module.config; cfg.Tick != 25*time.Millisecond || cfg.Sync != 2*time.Second || cfg.LockTTL != 3*time.Second || cfg.Settings["code"] != true || cfg.Settings["file"] != true {
				t.Fatalf("config %+v", cfg)
			}
		})
	}
	for _, job := range []Job{{Target: "tick", Schedule: "bad"}, {Schedule: "* * * * *"}, {Target: "tick", Schedule: "0 0 31 2 *"}} {
		m := newModule()
		if err := m.Upsert("bad", job); err == nil {
			t.Fatalf("accepted invalid job %+v", job)
		}
	}
}

func TestMemoryStoreSnapshotsLocksAndHistory(t *testing.T) {
	conn, _ := (&defaultDriver{}).Connection(&Instance{})
	job := Job{Target: "target", Schedule: "* * * * *", Payload: Map{"nested": Map{"value": "original"}}}
	if err := conn.Add("job", job); err != nil {
		t.Fatal(err)
	}
	job.Payload["nested"].(Map)["value"] = "mutated"
	jobs, _ := conn.List()
	if jobs["job"].Payload["nested"].(Map)["value"] != "original" {
		t.Fatal("store shares caller map")
	}
	jobs["job"].Payload["nested"].(Map)["value"] = "mutated"
	jobs, _ = conn.List()
	if jobs["job"].Payload["nested"].(Map)["value"] != "original" {
		t.Fatal("list shares stored map")
	}
	for i := 0; i < 15; i++ {
		if err := conn.AppendLog(Log{Job: "job", Payload: Map{"i": i}}); err != nil {
			t.Fatal(err)
		}
	}
	total, logs, err := conn.History("job", 1, 2)
	if err != nil || total != 10 || len(logs) != 2 || logs[0].Payload["i"] != 13 {
		t.Fatalf("history %d %+v %v", total, logs, err)
	}
	if locked, err := conn.Lock("one", time.Hour); err != nil || !locked {
		t.Fatal("first lock failed")
	}
	if locked, _ := conn.Lock("one", time.Hour); locked {
		t.Fatal("duplicate lock granted")
	}
	if err := conn.Disable("job"); err != nil {
		t.Fatal(err)
	}
	jobs, _ = conn.List()
	if !jobs["job"].Disabled {
		t.Fatal("not disabled")
	}
	if err := conn.Remove("job"); err != nil {
		t.Fatal(err)
	}
	total, _, _ = conn.History("job", 0, 0)
	if total != 0 {
		t.Fatal("history survived deletion")
	}
}

func TestDynamicJobsAndHistory(t *testing.T) {
	resetRuntime(t)
	seen := make(chan struct{}, 4)
	dubi.Register("tick", dubi.Method{Action: func(ctx *dubi.Context) { seen <- struct{}{} }})
	startRuntime(t)
	if err := Add("clock", Job{Target: "tick", Schedule: "@every 1s"}); err != nil {
		t.Fatal(err)
	}
	if err := Disable("clock"); err != nil {
		t.Fatal(err)
	}
	if len(module.schedules["clock"]) != 0 {
		t.Fatal("disabled job still scheduled")
	}
	if err := Enable("clock"); err != nil {
		t.Fatal(err)
	}
	receive(t, seen)
	// Wait for the job's history to be committed without stopping the store.
	deadline := time.Now().Add(time.Second)
	for {
		total, logs := ListLogs("clock", 0, 1)
		if total > 0 {
			if !logs[0].Success || logs[0].Target != "tick" {
				t.Fatalf("history %+v", logs)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("history not committed")
		}
		time.Sleep(time.Millisecond)
	}
	if err := Remove("clock"); err != nil {
		t.Fatal(err)
	}
	if len(ListJobs()) != 0 {
		t.Fatal("job not removed")
	}
	if err := Disable("missing"); err == nil {
		t.Fatal("missing job accepted")
	}
}

func TestStopCancelsActiveJobAndWaits(t *testing.T) {
	resetRuntime(t)
	entered := make(chan struct{})
	finished := make(chan struct{})
	dubi.Register("wait", dubi.Method{Action: func(ctx *dubi.Context) { close(entered); <-ctx.Context.Done(); close(finished) }})
	dubi.Register("wait.job", Job{Target: "wait", Schedule: "@every 1s"})
	startRuntime(t)
	receive(t, entered)
	ended := make(chan error, 1)
	go func() { ended <- dubi.End() }()
	receive(t, finished)
	if err := receive(t, ended); err != nil {
		t.Fatal(err)
	}
}
