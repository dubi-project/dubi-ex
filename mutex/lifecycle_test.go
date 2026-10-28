package mutex

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type probeDriver struct {
	fail             string
	connections      []*probeConnection
	entered, release chan struct{}
}

func (d *probeDriver) Connect(inst *Instance) (Connection, error) {
	conn := &probeConnection{name: inst.Name, driver: d}
	d.connections = append(d.connections, conn)
	if d.fail == "connect" && inst.Name == "b" {
		return conn, errors.New("connect failure")
	}
	return conn, nil
}

type probeConnection struct {
	name   string
	driver *probeDriver
	closed atomic.Bool
}

func (c *probeConnection) Open() error {
	if c.driver.fail == "open" && c.name == "b" {
		return errors.New("open failure")
	}
	return nil
}
func (c *probeConnection) Close() error { c.closed.Store(true); return nil }
func (c *probeConnection) Lock(string, time.Duration) error {
	if c.driver.entered != nil {
		close(c.driver.entered)
		<-c.driver.release
	}
	if c.closed.Load() {
		return errors.New("connection closed during operation")
	}
	return nil
}
func (*probeConnection) Unlock(string) error { return nil }

func TestOpenRollbackAndRetry(t *testing.T) {
	for _, phase := range []string{"connect", "open"} {
		t.Run(phase, func(t *testing.T) {
			m := newModule()
			driver := &probeDriver{fail: phase}
			m.RegisterDriver("probe", driver)
			m.RegisterConfigs(Configs{"a": {Type: "probe"}, "b": {Type: "probe"}})
			if err := m.Setup(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Open(nil); err == nil {
				t.Fatal("expected failure")
			}
			if m.opened || len(m.instances) > 0 {
				t.Fatal("partial connections survived")
			}
			for _, conn := range driver.connections {
				if !conn.closed.Load() {
					t.Fatalf("connection %s not closed", conn.name)
				}
			}
			driver.fail = ""
			if err := m.Open(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCloseWaitsForBackendOperation(t *testing.T) {
	m := newModule()
	driver := &probeDriver{entered: make(chan struct{}), release: make(chan struct{})}
	m.RegisterDriver("probe", driver)
	m.RegisterConfig("default", Config{Type: "probe"})
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	locked := make(chan error, 1)
	go func() { locked <- m.Lock("slow") }()
	await(t, driver.entered)
	closeStarted, closed := make(chan struct{}), make(chan error, 1)
	go func() { close(closeStarted); closed <- m.Close(nil) }()
	await(t, closeStarted)
	if driver.connections[0].closed.Load() {
		t.Fatal("connection closed before in-flight operation")
	}
	close(driver.release)
	if err := await(t, locked); err != nil {
		t.Fatal(err)
	}
	if err := await(t, closed); err != nil {
		t.Fatal(err)
	}
	if !driver.connections[0].closed.Load() {
		t.Fatal("connection not closed")
	}
}

func TestOptionalCapabilitiesReturnExplicitErrors(t *testing.T) {
	m := newModule()
	m.RegisterDriver("plain", &probeDriver{})
	m.RegisterConfig("default", Config{Type: "plain"})
	if err := m.Open(nil); err != nil {
		t.Fatal(err)
	}
	defer m.Close(nil)
	if err := m.Lock("key"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Locked("key"); !errors.Is(err, ErrUnsupportedCheck) {
		t.Fatalf("unsupported check: %v", err)
	}
	if err := m.Refresh("key"); !errors.Is(err, ErrUnsupportedRefresh) {
		t.Fatalf("unsupported refresh: %v", err)
	}
	cap, err := m.CapabilityFrom("default")
	if err != nil || cap != (Capability{}) {
		t.Fatalf("capability: %+v %v", cap, err)
	}
	if err := m.Unlock("key"); err != nil {
		t.Fatal(err)
	}
}
