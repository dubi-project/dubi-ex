package cache

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coocood/freecache"
	. "github.com/dubi-project/dubi-go/base"
)

func testModule(t *testing.T, configs Configs) *Module {
	t.Helper()
	m := &Module{}
	m.RegisterDriver("memory", &memoryDriver{})
	if len(configs) == 0 {
		configs = Configs{defaultName: {}}
	}
	for name, cfg := range configs {
		if cfg.Settings == nil {
			cfg.Settings = Map{"size": 1 << 20}
		}
		m.RegisterConfig(name, cfg)
	}
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(nil); err != nil {
			t.Error(err)
		}
	})
	return m
}

func TestCacheRoundTripAndStats(t *testing.T) {
	m := testModule(t, Configs{defaultName: {Prefix: "app:"}})
	if value, err := m.Read("missing"); err != nil || value != nil {
		t.Fatalf("miss: %#v %v", value, err)
	}
	value := Map{"user": Map{"name": "Ada"}, "tags": []any{"go", "dubi"}}
	if err := m.Write("user:1", value); err != nil {
		t.Fatal(err)
	}
	out, err := m.Read("user:1")
	if err != nil || !reflect.DeepEqual(out, Map{"user": map[string]any{"name": "Ada"}, "tags": []any{"go", "dubi"}}) {
		t.Fatalf("read: %#v %v", out, err)
	}
	if ok, err := m.Exists("user:1"); err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	keys, err := m.Keys("user:")
	if err != nil || !reflect.DeepEqual(keys, []string{"user:1"}) {
		t.Fatalf("keys: %v %v", keys, err)
	}
	if err := m.Delete("user:1"); err != nil {
		t.Fatal(err)
	}
	if data, err := m.ReadData("user:1"); err != nil || data != nil {
		t.Fatalf("deleted: %q %v", data, err)
	}
	stats := m.Stats()
	if stats.Read != 3 || stats.Write != 1 || stats.Exists != 1 || stats.Delete != 1 || stats.Keys != 1 || stats.Hit != 2 || stats.Miss != 2 || stats.Error != 0 {
		t.Fatalf("stats: %+v", stats)
	}
	if stats.Instances[defaultName].Read != 3 {
		t.Fatalf("source stats: %+v", stats)
	}
	m.ResetStats()
	if stats := m.Stats(); stats.Read != 0 || stats.Instances[defaultName].Read != 0 {
		t.Fatalf("reset stats: %+v", stats)
	}
}

func TestRawDataIsCopiedAndEmptyIsNotMissing(t *testing.T) {
	m := testModule(t, nil)
	input := []byte("hello")
	if err := m.WriteData("bytes", input); err != nil {
		t.Fatal(err)
	}
	input[0] = 'X'
	out, err := m.ReadData("bytes")
	if err != nil || string(out) != "hello" {
		t.Fatalf("copied input: %q %v", out, err)
	}
	out[0] = 'X'
	out, err = m.ReadData("bytes")
	if err != nil || string(out) != "hello" {
		t.Fatalf("copied output: %q %v", out, err)
	}
	if err := m.WriteData("empty", nil); err != nil {
		t.Fatal(err)
	}
	if out, err := m.ReadData("empty"); err != nil || out == nil || len(out) != 0 {
		t.Fatalf("empty: %#v %v", out, err)
	}
	if ok, err := m.Exists("empty"); !ok || err != nil {
		t.Fatalf("empty exists: %v %v", ok, err)
	}
}

func TestMultipleSourcesAndManualOnlySource(t *testing.T) {
	m := testModule(t, Configs{"a": {Weight: 1}, "b": {Weight: 3}, "manual": {Weight: -1}})
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key:%d", i)
		if err := m.WriteData(key, []byte(key)); err != nil {
			t.Fatal(err)
		}
		selected := m.hashring.Locate(key)
		if selected == "manual" {
			t.Fatal("manual-only source selected automatically")
		}
		for _, name := range []string{"a", "b", "manual"} {
			value, err := m.ReadDataFrom(name, key)
			if err != nil {
				t.Fatal(err)
			}
			if (value != nil) != (name == selected) {
				t.Fatalf("key %q: selected %q, found in %q", key, selected, name)
			}
		}
	}
	if err := m.WriteTo("manual", "only", Map{"manual": true}); err != nil {
		t.Fatal(err)
	}
	if out, err := m.ReadFrom("manual", "only"); err != nil || out["manual"] != true {
		t.Fatalf("explicit source: %v %v", out, err)
	}
	if ok, err := m.ExistsIn("manual", "only"); err != nil || !ok {
		t.Fatalf("exists in source: %v %v", ok, err)
	}
	if err := m.DeleteFrom("manual", "only"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadFrom("unknown", "key"); !errors.Is(err, ErrInvalidConnection) {
		t.Fatalf("unknown source: %v", err)
	}
	if err := m.ClearFrom("manual", "key:"); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyManualSourcesDoNotFallBack(t *testing.T) {
	m := testModule(t, Configs{"manual": {Weight: -1}})
	if err := m.WriteData("key", []byte("value")); !errors.Is(err, ErrInvalidConnection) {
		t.Fatalf("empty ring: %v", err)
	}
	if err := m.WriteDataTo("manual", "key", []byte("value")); err != nil {
		t.Fatal(err)
	}
}

func TestClearPreflightAndPrefixIsolation(t *testing.T) {
	m := testModule(t, Configs{"a": {Prefix: "app:", AllowClearAll: true}, "b": {Prefix: "app:"}})
	for _, name := range []string{"a", "b"} {
		if err := m.WriteDataTo(name, "keep", []byte("keep")); err != nil {
			t.Fatal(err)
		}
		if err := m.WriteDataTo(name, "drop:1", []byte("drop")); err != nil {
			t.Fatal(err)
		}
		if err := m.instances[name].connect.Write("other:1", []byte("outside prefix"), 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Clear(); !errors.Is(err, ErrUnsafeClear) {
		t.Fatalf("unsafe clear: %v", err)
	}
	for _, name := range []string{"a", "b"} {
		if ok, _ := m.ExistsIn(name, "keep"); !ok {
			t.Fatal("clear ran before all sources passed preflight")
		}
	}
	if err := m.Clear("drop:"); err != nil {
		t.Fatal(err)
	}
	if keys, err := m.Keys(); err != nil || !reflect.DeepEqual(keys, []string{"keep", "keep"}) {
		t.Fatalf("keys across sources: %v %v", keys, err)
	}
	if err := m.ClearAll(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if out, err := m.instances[name].connect.Read("other:1"); err != nil || out == nil {
			t.Fatalf("clear escaped configured prefix: %q %v", out, err)
		}
	}
}

type testClock struct{ now uint32 }

func (c *testClock) Now() uint32 { return c.now }

func TestExpirationAndSequenceReset(t *testing.T) {
	m := testModule(t, Configs{defaultName: {Expire: 2 * time.Second}})
	clock := &testClock{now: 100}
	conn := m.instances[defaultName].connect.(*memoryConnection)
	conn.cache = freecache.NewCacheCustomTimer(1<<20, clock)
	if err := m.WriteData("default", []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteData("short", []byte("value"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteData("permanent", []byte("value"), 0); err != nil {
		t.Fatal(err)
	}
	if value, err := m.Sequence("sequence", 10, 2, time.Second); err != nil || value != 10 {
		t.Fatalf("first sequence: %d %v", value, err)
	}
	clock.now++
	if data, _ := m.ReadData("short"); data != nil {
		t.Fatal("subsecond TTL became permanent")
	}
	if data, _ := m.ReadData("default"); data == nil {
		t.Fatal("default TTL expired early")
	}
	if value, err := m.Sequence("sequence", 10, 2, time.Second); err != nil || value != 10 {
		t.Fatalf("expired sequence: %d %v", value, err)
	}
	clock.now++
	if data, _ := m.ReadData("default"); data != nil {
		t.Fatal("default TTL was ignored")
	}
	if keys, err := m.Keys(); err != nil || !reflect.DeepEqual(keys, []string{"permanent"}) {
		t.Fatalf("expired keys: %v %v", keys, err)
	}
}

func TestSequenceBatchesAreAtomicAndOverflowDoesNotCommit(t *testing.T) {
	m := testModule(t, nil)
	var wg sync.WaitGroup
	results := make(chan []int64, 32)
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			values, err := m.SequenceMany("counter", 0, 1, 10)
			if err != nil {
				t.Error(err)
				return
			}
			results <- values
		})
	}
	wg.Wait()
	close(results)
	var all []int64
	for values := range results {
		for i := 1; i < len(values); i++ {
			if values[i] != values[i-1]+1 {
				t.Fatalf("interleaved batch: %v", values)
			}
		}
		all = append(all, values...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	if len(all) != 320 {
		t.Fatalf("count = %d", len(all))
	}
	for i, value := range all {
		if value != int64(i) {
			t.Fatalf("sequence gap at %d: %d", i, value)
		}
	}
	if _, err := m.SequenceMany("counter", 0, 1, math.MaxInt64); !errors.Is(err, ErrInvalidCount) {
		t.Fatalf("oversized batch: %v", err)
	}
	if err := m.WriteData("overflow", []byte(fmt.Sprint(math.MaxInt64-1))); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SequenceMany("overflow", 0, 1, 2); !errors.Is(err, ErrSequenceOverflow) {
		t.Fatalf("overflow: %v", err)
	}
	if value, err := m.ReadData("overflow"); err != nil || string(value) != fmt.Sprint(math.MaxInt64-1) {
		t.Fatalf("overflow changed counter: %s %v", value, err)
	}
	if err := m.WriteData("bad", []byte("not an integer")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Sequence("bad", 0, 1); err == nil {
		t.Fatal("invalid sequence value accepted")
	}
	if value, err := m.Sequence("negative", 0, -2); err != nil || value != 0 {
		t.Fatalf("sequence starting at zero: %d %v", value, err)
	}
	if value, err := m.Sequence("negative", 0, -2); err != nil || value != -2 {
		t.Fatalf("negative step: %d %v", value, err)
	}
}

// A third-party driver need not implement ManyConnect.
type singleSequenceDriver struct{}
type singleSequenceConnection struct{ Connect }

func (*singleSequenceDriver) Connect(inst *Instance) (Connect, error) {
	conn, err := (&memoryDriver{}).Connect(inst)
	return &singleSequenceConnection{Connect: conn}, err
}

func TestSequenceManyFallback(t *testing.T) {
	m := &Module{}
	m.RegisterDriver("single", &singleSequenceDriver{})
	m.RegisterConfig("default", Config{Type: "single", Settings: Map{"size": 1 << 20}})
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	defer m.Close(nil)
	if values, err := m.SequenceMany("key", 1, 2, 3); err != nil || !reflect.DeepEqual(values, []int64{1, 3, 5}) {
		t.Fatalf("fallback: %v %v", values, err)
	}
}

func TestUnavailableAndClosedModuleOperations(t *testing.T) {
	m := &Module{}
	if _, err := m.Read("key"); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("before open: %v", err)
	}
	if _, err := m.Keys(); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("keys before open: %v", err)
	}
	if err := m.ClearAll(); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("clear before open: %v", err)
	}
	m = testModule(t, nil)
	if err := m.Close(nil); err != nil {
		t.Fatal(err)
	}
	if m.Ready() || !errors.Is(m.Health(nil), ErrNotOpen) {
		t.Fatal("closed module reported ready")
	}
	if err := m.Write("key", Map{}); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("after close: %v", err)
	}
}

func TestConcurrentOperationsAndClose(t *testing.T) {
	m := testModule(t, nil)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < 8; worker++ {
		wg.Go(func() {
			<-start
			for i := 0; i < 200; i++ {
				key := fmt.Sprint(i)
				if err := m.WriteData(key, []byte("value")); err != nil && !errors.Is(err, ErrNotOpen) {
					t.Error(err)
				}
				if _, err := m.ReadData(key); err != nil && !errors.Is(err, ErrNotOpen) {
					t.Error(err)
				}
				if i%20 == 0 {
					m.Stats()
					m.ResetStats()
				}
			}
		})
	}
	close(start)
	if err := m.Close(nil); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestKeyBuilder(t *testing.T) {
	if got := Key("user", 12, "profile"); got != "user:12:profile" {
		t.Fatal(got)
	}
	if got := KeyWith("/", "user", nil, 12); got != "user//12" {
		t.Fatal(got)
	}
	if got := Key(); got != "" {
		t.Fatal(got)
	}
}

func TestCodecErrorsAndDriverErrorsAreVisible(t *testing.T) {
	m := testModule(t, nil)
	if err := m.Write("bad", Map{"fn": func() {}}); err == nil {
		t.Fatal("JSON encode error was swallowed")
	}
	if err := m.WriteData("bad", []byte("{")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Read("bad"); err == nil {
		t.Fatal("JSON decode error was swallowed")
	}
	if err := m.WriteData("oversized", []byte(strings.Repeat("x", 2048))); err == nil {
		t.Fatal("memory driver error was swallowed")
	}
	if stats := m.Stats(); stats.Error != 3 {
		t.Fatalf("error stats: %+v", stats)
	}
}
