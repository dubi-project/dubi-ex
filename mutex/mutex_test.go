package mutex

import (
	"context"
	"errors"
	"sync"
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
func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for mutex")
		var zero T
		return zero
	}
}
func clock(t *testing.T) func(time.Duration) {
	t.Helper()
	conn := module.instances["default"].conn.(*defaultConnect)
	var offset atomic.Int64
	now := time.Now()
	conn.now = func() time.Time { return now.Add(time.Duration(offset.Load())) }
	return func(delta time.Duration) { offset.Add(int64(delta)) }
}

func TestDefaultLockAndNonDestructiveCheck(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	if !Ready() {
		t.Fatal("module not ready")
	}
	if Key("order", 12) != "order-12" || KeyWith(":", "order", 12, "pay") != "order:12:pay" {
		t.Fatal("key construction")
	}
	lk, err := Lock("order", 12, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !Locked("order", 12) {
		t.Fatal("lock missing")
	}
	if _, err := TryLock("order", 12); !errors.Is(err, ErrLocked) {
		t.Fatalf("contention: %v", err)
	}
	if err := lk.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := lk.Unlock(); err != nil {
		t.Fatal("handle release should be idempotent")
	}
	if locked, err := Check("order", 12); err != nil || locked {
		t.Fatalf("unlock check: %v %v", locked, err)
	}
	if err := lk.Refresh(); !errors.Is(err, ErrLostLock) {
		t.Fatalf("released refresh: %v", err)
	}
}

func TestStaleHandleAndHelperCannotReleaseNewOwner(t *testing.T) {
	for _, helper := range []bool{false, true} {
		t.Run(map[bool]string{false: "handle", true: "helper"}[helper], func(t *testing.T) {
			resetRuntime(t)
			startRuntime(t)
			advance := clock(t)
			first, err := Lock("job", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			advance(2 * time.Second)
			second, err := Lock("job", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if !helper {
				if err := first.Refresh(); !errors.Is(err, ErrLostLock) {
					t.Fatalf("stale refresh: %v", err)
				}
				if err := first.Unlock(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := Unlock("job"); err != nil {
					t.Fatal(err)
				}
			}
			if !Locked("job") {
				t.Fatal("old owner released new owner's lock")
			}
			if err := second.Unlock(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRefreshAndKeepAlive(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	advance := clock(t)
	lk, err := Lock("refresh", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	advance(750 * time.Millisecond)
	if err := lk.Refresh(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	advance(time.Second)
	if !Locked("refresh") {
		t.Fatal("lease was not extended")
	}
	if err := lk.Unlock(); err != nil {
		t.Fatal(err)
	}
	lk, err = Lock("keepalive", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := lk.KeepAlive(5 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	baseline := Stats().Refresh
	advance(750 * time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for Stats().Refresh == baseline {
		if time.Now().After(deadline) {
			t.Fatal("renewal did not run")
		}
		time.Sleep(time.Millisecond)
	}
	advance(500 * time.Millisecond)
	if !Locked("keepalive") {
		t.Fatal("automatic lease was not extended")
	}
	if err := lk.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := lk.KeepAlive(); !errors.Is(err, ErrLostLock) {
		t.Fatalf("released keepalive: %v", err)
	}
}

func TestWaitLockTimeoutCancelAndClose(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	lk, err := Lock("wait", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WaitLock(10*time.Millisecond, time.Millisecond, "wait"); !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WaitLockContext(ctx, time.Millisecond, "free"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled free acquisition: %v", err)
	}
	if Locked("free") {
		t.Fatal("canceled waiter acquired a lease")
	}
	acquired := make(chan *locker, 1)
	go func() {
		next, err := WaitLock(time.Second, time.Millisecond, "wait")
		if err == nil {
			acquired <- next
		}
	}()
	if err := lk.Unlock(); err != nil {
		t.Fatal(err)
	}
	lk = await(t, acquired)
	ended := make(chan error, 1)
	go func() { _, err := WaitLockContext(context.Background(), time.Hour, "wait"); ended <- err }()
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	if err := await(t, ended); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed waiter: %v", err)
	}
	if err := lk.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed handle: %v", err)
	}
}

func TestConcurrentContentionAndKeepAliveShutdown(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	var winners atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := TryLock("one", time.Minute); err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrLocked) {
				t.Errorf("lock: %v", err)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("owners: %d", winners.Load())
	}
	lk, err := Lock("renew", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := lk.KeepAlive(time.Hour); err != nil {
		t.Fatal(err)
	}
	done := lk.keepAliveDone
	if err := dubi.End(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("keepalive survived Close")
	}
}

func TestHandleDoesNotOperateOnAnotherRuntimeSession(t *testing.T) {
	resetRuntime(t)
	startRuntime(t)
	old, err := Lock("session", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	dubi.Reset()
	if err := dubi.DefaultRuntime().Run(); err != nil {
		t.Fatal(err)
	}
	current, err := Lock("session", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Unlock(); !errors.Is(err, ErrClosed) {
		t.Fatalf("old handle accepted in new session: %v", err)
	}
	if err := old.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("old refresh accepted: %v", err)
	}
	if !Locked("session") {
		t.Fatal("old session removed new lease")
	}
	if err := current.Unlock(); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsCapabilitiesAndSnapshots(t *testing.T) {
	resetRuntime(t)
	dubi.Register("default", Config{Settings: Map{"nested": Map{"value": "original"}}})
	startRuntime(t)
	lk, err := Lock("order:1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock("order:1"); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if _, err := Check("order:1"); err != nil {
		t.Fatal(err)
	}
	if err := lk.Refresh(); err != nil {
		t.Fatal(err)
	}
	stats := Stats()
	if stats.Lock != 1 || stats.Refresh != 1 || stats.Check != 1 || stats.Contention != 1 || stats.ContentionByPrefix["order"] != 1 || stats.ActiveTokens != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	cap, err := CapabilityFrom("default")
	if err != nil || !cap.Token || !cap.TokenRefresh || !cap.Check || !cap.Refresh {
		t.Fatalf("capabilities: %+v %v", cap, err)
	}
	debug := Debug()
	if !debug.Opened || debug.Instances["default"].Type != "memory" || debug.Stats.ActiveTokens != 1 {
		t.Fatalf("debug: %+v", debug)
	}
	debug.Instances["default"].Settings["nested"].(Map)["value"] = "mutated"
	if Debug().Instances["default"].Settings["nested"].(Map)["value"] != "original" {
		t.Fatal("debug shares mutable config")
	}
	if len(DebugTokens()) != 1 {
		t.Fatal("missing token debug")
	}
	if err := lk.Unlock(); err != nil {
		t.Fatal(err)
	}
	if Stats().ActiveTokens != 0 {
		t.Fatal("released token retained")
	}
	ResetStats()
	if Stats().Lock != 0 {
		t.Fatal("stats not reset")
	}
	module.token.Lock()
	module.tokens[tokenQueueKey("default", "expired")] = []tokenEntry{{token: "expired", until: time.Now().Add(-2 * time.Minute), grace: time.Minute}}
	module.token.Unlock()
	if removed := module.cleanupExpiredTokens(); removed != 1 {
		t.Fatalf("cleanup removed %d", removed)
	}
}
