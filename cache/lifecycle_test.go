package cache

import (
	"errors"
	"reflect"
	"testing"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

type lifecycleDriver struct {
	events    []string
	failOpen  bool
	failClose error
}

type blockedReadDriver struct {
	entered chan struct{}
	release chan struct{}
}

type blockedReadConnection struct {
	Connect
	owner *blockedReadDriver
}

func (d *blockedReadDriver) Connect(inst *Instance) (Connect, error) {
	conn, err := (&memoryDriver{}).Connect(inst)
	if err != nil {
		return nil, err
	}
	return &blockedReadConnection{Connect: conn, owner: d}, nil
}

func (c *blockedReadConnection) Read(key string) ([]byte, error) {
	close(c.owner.entered)
	<-c.owner.release
	return c.Connect.Read(key)
}

func TestCloseWaitsForInFlightOperation(t *testing.T) {
	driver := &blockedReadDriver{entered: make(chan struct{}), release: make(chan struct{})}
	m := &Module{}
	m.RegisterDriver("blocked", driver)
	m.RegisterConfig("default", Config{Type: "blocked", Settings: Map{"size": 1 << 20}})
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteData("key", []byte("value")); err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		value, err := m.ReadData("key")
		if err == nil && string(value) != "value" {
			err = errors.New("read lost its connection during Close")
		}
		readDone <- err
	}()
	<-driver.entered
	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() { close(closeStarted); closeDone <- m.Close(nil) }()
	<-closeStarted
	select {
	case err := <-closeDone:
		close(driver.release)
		t.Fatalf("Close returned with operation still running: %v", err)
	default:
	}
	close(driver.release)
	for _, done := range []<-chan error{readDone, closeDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cache operation or Close deadlocked")
		}
	}
}

type lifecycleConnection struct {
	Connect
	owner *lifecycleDriver
	name  string
}

func (d *lifecycleDriver) Connect(inst *Instance) (Connect, error) {
	conn, err := (&memoryDriver{}).Connect(inst)
	if err != nil {
		return nil, err
	}
	return &lifecycleConnection{Connect: conn, owner: d, name: inst.Name}, nil
}

func (c *lifecycleConnection) Open() error {
	c.owner.events = append(c.owner.events, "open:"+c.name)
	if c.name == "b" && c.owner.failOpen {
		return errors.New("test open failure")
	}
	return c.Connect.Open()
}

func (c *lifecycleConnection) Close() error {
	c.owner.events = append(c.owner.events, "close:"+c.name)
	return errors.Join(c.Connect.Close(), c.owner.failClose)
}

func TestOpenRollbackAndRetry(t *testing.T) {
	driver := &lifecycleDriver{failOpen: true}
	m := &Module{}
	m.RegisterDriver("test", driver)
	m.RegisterConfigs(Configs{"a": {Type: "test", Settings: Map{"size": 1 << 20}}, "b": {Type: "test", Settings: Map{"size": 1 << 20}}})
	if err := m.Open(nil); err == nil {
		t.Fatal("expected startup failure")
	}
	want := []string{"open:a", "open:b", "close:b", "close:a"}
	if !reflect.DeepEqual(driver.events, want) {
		t.Fatalf("rollback: %v", driver.events)
	}
	if m.Ready() || len(m.instances) != 0 {
		t.Fatal("partial open leaked into active sources")
	}
	driver.failOpen = false
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("test close failure")
	driver.failClose = closeErr
	if err := m.Close(nil); !errors.Is(err, closeErr) {
		t.Fatalf("close error lost: %v", err)
	}
	if !reflect.DeepEqual(driver.events[len(want):], []string{"open:a", "open:b", "close:b", "close:a"}) {
		t.Fatalf("close did not attempt every source: %v", driver.events)
	}
	if err := m.Close(nil); err != nil {
		t.Fatalf("close is not idempotent: %v", err)
	}
}
