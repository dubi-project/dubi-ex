package event

import (
	"errors"
	"testing"

	. "github.com/dubi-project/dubi-go/base"
)

type failDriver struct {
	connections                       []*failConnection
	failOpen, failStart, failRegister string
}

func (d *failDriver) Connect(inst *Instance) (Connection, error) {
	conn := &failConnection{name: inst.Name, driver: d}
	d.connections = append(d.connections, conn)
	return conn, nil
}

type failConnection struct {
	name                             string
	driver                           *failDriver
	opened, closed, started, stopped bool
}

func (c *failConnection) Open() error {
	c.opened = true
	if c.name == c.driver.failOpen {
		return errors.New("open failure")
	}
	return nil
}
func (c *failConnection) Close() error { c.closed = true; return nil }
func (c *failConnection) Start() error {
	c.started = true
	if c.name == c.driver.failStart {
		return errors.New("start failure")
	}
	return nil
}
func (c *failConnection) Stop() error { c.stopped = true; return nil }
func (c *failConnection) Register(string, string) error {
	if c.name == c.driver.failRegister {
		return errors.New("register failure")
	}
	return nil
}
func (*failConnection) Publish(string, []byte) error { return nil }

func TestOpenAndStartFailureRollback(t *testing.T) {
	for _, phase := range []string{"open", "register", "start"} {
		t.Run(phase, func(t *testing.T) {
			m := newModule()
			driver := &failDriver{}
			switch phase {
			case "open":
				driver.failOpen = "b"
			case "register":
				driver.failRegister = "b"
			case "start":
				driver.failStart = "b"
			}
			m.RegisterDriver("record", driver)
			m.RegisterConfig("a", Config{Type: "record"})
			m.RegisterConfig("b", Config{Type: "record"})
			m.RegisterEvent("job", Event{})
			if err := m.Setup(nil); err != nil {
				t.Fatal(err)
			}
			openErr := m.Open(nil)
			if phase != "start" {
				if openErr == nil {
					t.Fatal("expected open error")
				}
				if m.opened || len(m.instances) != 0 {
					t.Fatal("partial state survived open")
				}
				for _, conn := range driver.connections {
					if !conn.closed {
						t.Fatalf("source %s not closed", conn.name)
					}
				}
			} else {
				if openErr != nil {
					t.Fatal(openErr)
				}
				if err := m.Start(nil); err == nil {
					t.Fatal("expected start error")
				}
				for _, conn := range driver.connections {
					if conn.started && !conn.stopped {
						t.Fatalf("source %s not stopped", conn.name)
					}
				}
				if err := m.Close(nil); err != nil {
					t.Fatal(err)
				}
				for _, conn := range driver.connections {
					if !conn.closed {
						t.Fatal("source not closed")
					}
				}
			}
			driver.failOpen = ""
			driver.failRegister = ""
			driver.failStart = ""
			if err := m.Open(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Start(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestConfigurationRejectsInvalidValues(t *testing.T) {
	for _, fields := range []Map{{"sources": "bad"}, {"settings": "bad"}, {"sources": Map{"a": Map{"type": "missing"}}}, {"weight": -2}, {"codec": "missing"}, {"sources": Map{"default": Map{}}, "type": "memory"}, {"external": "true"}} {
		m := newModule()
		m.RegisterDriver("memory", &defaultDriver{})
		err := m.Config(Map{"event": fields})
		if err == nil {
			err = m.Setup(nil)
		}
		if err == nil {
			t.Fatalf("accepted config %v", fields)
		}
	}
	for _, name := range []string{"", "bad..name", "bad name", ".bad", "bad."} {
		if err := validateName(name); err == nil {
			t.Fatalf("accepted name %q", name)
		}
	}
}
func TestConfigAliasesAndExplicitFalseOverrides(t *testing.T) {
	m := newModule()
	m.RegisterDriver("memory", &defaultDriver{})
	if err := m.Config(Map{"event": Map{"type": "memory", "external": false, "settings": Map{"same": "plural"}, "setting": Map{"same": "singular", "old": true}}}); err != nil {
		t.Fatal(err)
	}
	m.RegisterConfig("default", Config{External: true, Prefix: "code:", Settings: Map{"code": true}})
	if err := m.Setup(nil); err != nil {
		t.Fatal(err)
	}
	cfg := m.sources["default"]
	if cfg.External || cfg.Prefix != "code:" || cfg.Settings["same"] != "plural" || cfg.Settings["old"] != true || cfg.Settings["code"] != true {
		t.Fatalf("config %+v", cfg)
	}
}
