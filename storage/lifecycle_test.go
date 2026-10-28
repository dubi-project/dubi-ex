package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dubi "github.com/dubi-project/dubi-go"
)

type testDriver struct {
	fail        string
	connections []*testConnection
	blocking    bool
}

func (d *testDriver) Connect(inst *Instance) (Connection, error) {
	conn := &testConnection{instance: inst, driver: d, objects: map[string][]byte{}}
	d.connections = append(d.connections, conn)
	if d.fail == "connect" && inst.Name == "b" {
		return conn, errors.New("connect failure")
	}
	return conn, nil
}

type testConnection struct {
	instance *Instance
	driver   *testDriver
	mu       sync.Mutex
	objects  map[string][]byte
	closed   atomic.Bool
	fetches  atomic.Int64
	failRead bool
	last     *blockingBody
}

func (c *testConnection) Open() error {
	if c.driver.fail == "open" && c.instance.Name == "b" {
		return errors.New("open failure")
	}
	return nil
}
func (c *testConnection) Close() error { c.closed.Store(true); return nil }
func (c *testConnection) Upload(reader io.Reader, opt UploadOption) (*File, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	file := c.instance.NewFile(opt.Prefix, opt.Key, opt.Type, int64(len(data)))
	c.mu.Lock()
	c.objects[file.File()] = data
	c.mu.Unlock()
	return file, nil
}
func (c *testConnection) Fetch(file *File, opt FetchOption) (Stream, error) {
	c.fetches.Add(1)
	if c.driver.blocking {
		body := &blockingBody{done: make(chan struct{})}
		c.last = body
		return body, nil
	}
	c.mu.Lock()
	data, ok := c.objects[file.File()]
	data = append([]byte(nil), data...)
	fail := c.failRead
	c.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	if fail {
		return io.NopCloser(&failingReader{}), nil
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (c *testConnection) Remove(file *File, opt RemoveOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.objects, file.File())
	return nil
}

type blockingBody struct {
	done chan struct{}
	once sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) { <-b.done; return 0, io.ErrClosedPipe }
func (b *blockingBody) Close() error             { b.once.Do(func() { close(b.done) }); return nil }

func TestOptionalStreamingAndNoStatCacheReuse(t *testing.T) {
	resetRuntime(t)
	driver := &testDriver{}
	RegisterDriver("test-stream", driver)
	dubi.Register("default", Config{Type: "test-stream"})
	startRuntime(t)
	file, err := UploadReader(bytes.NewReader([]byte("original")), UploadOption{Key: "key"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := Fetch(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body.(io.Seeker); ok {
		t.Fatal("artificial seeking requirement")
	}
	body.Close()
	if _, err := Local(file.Code()); !errors.Is(err, ErrLocalUnsupported) {
		t.Fatalf("local unsupported: %v", err)
	}
	conn := driver.connections[0]
	if _, err := Download(file.Code()); err != nil {
		t.Fatal(err)
	}
	count := conn.fetches.Load()
	if _, err := Download(file.Code()); err != nil {
		t.Fatal(err)
	}
	if conn.fetches.Load() != count+1 {
		t.Fatal("unversioned backend reused persistent cache")
	}
	target := filepath.Join(t.TempDir(), "export")
	os.WriteFile(target, []byte("previous valid export"), 0600)
	conn.mu.Lock()
	conn.failRead = true
	conn.mu.Unlock()
	if _, err := Download(file.Code(), DownloadOption{Target: target}); err == nil {
		t.Fatal("accepted partial download")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "previous valid export" {
		t.Fatal("failed download destroyed export")
	}
	if _, err := Download(file.Code()); err == nil {
		t.Fatal("accepted partial cache")
	}
	conn.mu.Lock()
	conn.failRead = false
	conn.mu.Unlock()
	name, err := Download(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(name)
	if string(data) != "original" {
		t.Fatalf("partial cache reused: %q", data)
	}
}
func TestCloseCancelsAndClosesOutstandingStreams(t *testing.T) {
	resetRuntime(t)
	driver := &testDriver{blocking: true}
	RegisterDriver("blocking", driver)
	dubi.Register("default", Config{Type: "blocking"})
	startRuntime(t)
	file, err := UploadReader(bytes.NewReader([]byte("x")), UploadOption{Key: "key"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := Fetch(file.Code())
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- dubi.End() }()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited on forgotten stream")
	}
	if !driver.connections[0].closed.Load() {
		t.Fatal("backend not closed")
	}
	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed stream: %v", err)
	}
	body.Close()
}
func TestOpenFailureRollback(t *testing.T) {
	for _, phase := range []string{"connect", "open"} {
		t.Run(phase, func(t *testing.T) {
			resetRuntime(t)
			driver := &testDriver{fail: phase}
			RegisterDriver("failing", driver)
			dubi.Register("sources", Configs{"a": {Type: "failing"}, "b": {Type: "failing"}})
			if err := dubi.DefaultRuntime().Run(); err == nil {
				t.Fatal("expected failure")
			}
			for _, conn := range driver.connections {
				if !conn.closed.Load() {
					t.Fatalf("source %s retained", conn.instance.Name)
				}
			}
			driver.fail = ""
			if err := dubi.DefaultRuntime().Run(); err != nil {
				t.Fatal(err)
			}
			if err := dubi.End(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
