package cron

import (
	"errors"
	"testing"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

type failDriver struct{ connection *failConnection }

func (d *failDriver) Connection(*Instance) (Connection, error) { return d.connection, nil }

type failConnection struct {
	*defaultConnect
	fail   string
	closed bool
}

func (c *failConnection) Open() error {
	if c.fail == "open" {
		return errors.New("open failed")
	}
	return nil
}
func (c *failConnection) Close() error { c.closed = true; return nil }
func (c *failConnection) Add(name string, job Job) error {
	if c.fail == "add" {
		return errors.New("add failed")
	}
	return c.defaultConnect.Add(name, job)
}
func (c *failConnection) List() (map[string]Job, error) {
	if c.fail == "list" {
		return nil, errors.New("list failed")
	}
	return c.defaultConnect.List()
}

func TestOpenFailureRollbackAndRetry(t *testing.T) {
	for _, phase := range []string{"open", "list", "add"} {
		t.Run(phase, func(t *testing.T) {
			m := newModule()
			conn := &failConnection{defaultConnect: &defaultConnect{jobs: map[string]Job{}, locks: map[string]time.Time{}, logs: map[string][]Log{}}, fail: phase}
			m.RegisterDriver("record", &failDriver{connection: conn})
			m.RegisterConfig(Config{Type: "record"})
			m.RegisterJob("job", Job{Target: "target", Schedule: "@every 1s"})
			if err := m.Setup(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Open(nil); err == nil {
				t.Fatal("expected open error")
			}
			if !conn.closed || m.connect != nil {
				t.Fatal("failed connection was retained")
			}
			conn.fail = ""
			conn.closed = false
			if err := m.Open(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Start(nil); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(nil); err != nil {
				t.Fatal(err)
			}
			if !conn.closed {
				t.Fatal("connection not closed")
			}
		})
	}
}

func TestInvalidConfigAndFailedAddDoNotMutateJobs(t *testing.T) {
	for _, fields := range []Map{{"tick": "bad"}, {"tick": -1}, {"type": "missing"}, {"settings": true}, {"unknown": 1}} {
		m := newModule()
		m.RegisterDriver("memory", &defaultDriver{})
		err := m.Config(Map{"cron": fields})
		if err == nil {
			err = m.Setup(nil)
		}
		if err == nil {
			t.Fatalf("accepted config %v", fields)
		}
	}
	m := newModule()
	conn := &failConnection{defaultConnect: &defaultConnect{jobs: map[string]Job{}}, fail: "add"}
	m.connect = conn
	if err := m.Upsert("job", Job{Target: "target", Schedule: "@every 1s"}); err == nil {
		t.Fatal("expected add failure")
	}
	if len(m.jobs) != 0 {
		t.Fatal("failed Add mutated local jobs")
	}
}
