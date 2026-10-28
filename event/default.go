package event

import (
	"sync"

	"github.com/dubi-project/dubi-ex/internal/support"
)

type defaultDriver struct{}
type defaultConnection struct {
	mutex             sync.RWMutex
	instance          *Instance
	events            map[string]chan []byte
	done              chan struct{}
	running, stopping bool
	wg                sync.WaitGroup
}

func (*defaultDriver) Connect(inst *Instance) (Connection, error) {
	return &defaultConnection{instance: inst, events: map[string]chan []byte{}, done: make(chan struct{})}, nil
}
func (*defaultConnection) Open() error    { return nil }
func (c *defaultConnection) Close() error { return c.Stop() }
func (c *defaultConnection) Register(name, group string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.running || c.stopping {
		return ErrEventRunning
	}
	if c.events[name] != nil {
		return nil
	}
	size := 64
	if v, err := support.Integer(c.instance.Settings["buffer"]); err == nil && v > 0 {
		if v > 1048576 {
			return ErrQueueFull
		}
		size = int(v)
	}
	if c.instance.Config.Queue > 0 {
		size = c.instance.Config.Queue
	}
	if size > 1048576 {
		return ErrQueueFull
	}
	c.events[name] = make(chan []byte, size)
	return nil
}
func (c *defaultConnection) Start() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.running || c.stopping {
		return ErrEventRunning
	}
	c.done = make(chan struct{})
	done := c.done
	for name, ch := range c.events {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			defer func() {
				// No publisher can enqueue once Stop marks this connection as
				// stopped. Submit the finite backlog before joining workers.
				for {
					select {
					case data := <-ch:
						c.instance.Serve(name, data)
					default:
						return
					}
				}
			}()
			for {
				select {
				case <-done:
					return
				default:
				}
				select {
				case <-done:
					return
				case data := <-ch:
					c.instance.Serve(name, data)
				}
			}
		}()
	}
	c.running = true
	return nil
}
func (c *defaultConnection) Stop() error {
	c.mutex.Lock()
	if !c.running {
		c.mutex.Unlock()
		return nil
	}
	c.running = false
	c.stopping = true
	close(c.done)
	c.mutex.Unlock()
	c.wg.Wait()
	c.mutex.Lock()
	c.stopping = false
	c.mutex.Unlock()
	return nil
}
func (c *defaultConnection) Publish(name string, data []byte) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if !c.running || c.stopping {
		return ErrNotRunning
	}
	ch := c.events[name]
	if ch == nil {
		return nil
	} // Pub/sub with no subscriber is a no-op.
	select {
	case ch <- append([]byte(nil), data...):
		return nil
	default:
		return ErrQueueFull
	}
}
