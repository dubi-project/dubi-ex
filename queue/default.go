package queue

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
)

type defaultDriver struct{}
type defaultMessage struct {
	name    string
	data    []byte
	attempt int
}
type defaultConnection struct {
	mutex                      sync.RWMutex
	instance                   *Instance
	queues                     map[string]chan *defaultMessage
	workers                    map[string]int
	timers                     map[*time.Timer]struct{}
	done                       chan struct{}
	running, stopping, stopped bool
	wg                         sync.WaitGroup
	timerWg                    sync.WaitGroup
}

func (*defaultDriver) Connect(inst *Instance) (Connection, error) {
	return &defaultConnection{instance: inst, queues: map[string]chan *defaultMessage{}, workers: map[string]int{}, timers: map[*time.Timer]struct{}{}, done: make(chan struct{})}, nil
}
func (*defaultConnection) Open() error    { return nil }
func (c *defaultConnection) Close() error { return c.Stop() }
func (c *defaultConnection) Register(name string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.running || c.stopping {
		return fmt.Errorf("queue: register while running")
	}
	if c.queues[name] == nil {
		size := 256
		if value, err := support.Integer(c.instance.Settings["buffer"]); err == nil && value > 0 {
			if value > 1048576 {
				return ErrQueueFull
			}
			size = int(value)
		}
		logical := strings.TrimPrefix(name, c.instance.Config.Prefix)
		if cfg, ok := c.instance.module.queues[logical]; ok {
			if value, err := support.Integer(cfg.Settings["buffer"]); err == nil && value > 0 {
				if value > 1048576 {
					return ErrQueueFull
				}
				size = int(value)
			}
		}
		c.queues[name] = make(chan *defaultMessage, size)
	}
	c.workers[name]++
	return nil
}
func (c *defaultConnection) Start() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.running || c.stopping {
		return fmt.Errorf("queue: already running")
	}
	if c.stopped {
		c.done = make(chan struct{})
		c.stopped = false
	}
	done := c.done
	for name, count := range c.workers {
		ch := c.queues[name]
		for i := 0; i < count; i++ {
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				for {
					select {
					case <-done:
						return
					default:
					}
					select {
					case <-done:
						return
					case msg := <-ch:
						resp := c.instance.Serve(Request{Name: msg.name, Data: msg.data, Attempt: msg.attempt, Timestamp: time.Now()})
						if resp.Retry {
							msg.attempt++
							delay := resp.Delay
							if delay <= 0 {
								delay = time.Millisecond
							}
							if err := c.publish(msg, delay, false, 0); err != nil {
								if err != ErrNotRunning {
									logError("retry", msg.name, err)
								}
							}
						}
					}
				}
			}()
		}
	}
	c.running = true
	return nil
}
func (c *defaultConnection) Stop() error {
	c.mutex.Lock()
	if c.stopped {
		c.mutex.Unlock()
		return nil
	}
	c.stopping = true
	c.running = false
	c.stopped = true
	close(c.done)
	for timer := range c.timers {
		if timer.Stop() {
			c.timerWg.Done()
		}
		delete(c.timers, timer)
	}
	c.mutex.Unlock()
	c.wg.Wait()
	c.timerWg.Wait()
	c.mutex.Lock()
	c.stopping = false
	c.mutex.Unlock()
	return nil
}

func (c *defaultConnection) Publish(name string, data []byte) error {
	wait, _ := c.instance.Settings["blocking_publish"].(bool)
	timeout := time.Duration(0)
	if v, ok := c.instance.Settings["publish_timeout"]; ok {
		if d, err := support.Duration(v); err == nil && d > 0 {
			timeout = d
			wait = true
		}
	}
	return c.publish(&defaultMessage{name: name, data: append([]byte(nil), data...), attempt: 1}, 0, wait, timeout)
}
func (c *defaultConnection) DeferredPublish(name string, data []byte, delay time.Duration) error {
	return c.publish(&defaultMessage{name: name, data: append([]byte(nil), data...), attempt: 1}, delay, false, 0)
}

func (c *defaultConnection) publish(msg *defaultMessage, delay time.Duration, wait bool, timeout time.Duration) error {
	c.mutex.Lock()
	if c.stopped || c.stopping {
		c.mutex.Unlock()
		return ErrNotRunning
	}
	ch := c.queues[msg.name]
	done := c.done
	if ch == nil {
		c.mutex.Unlock()
		return ErrInvalidQueue
	}
	if delay > 0 {
		// Schedule and record under the same lock. Even an immediately firing
		// callback cannot observe a partially initialized timer or escape Stop.
		c.timerWg.Add(1)
		var timer *time.Timer
		timer = time.AfterFunc(delay, func() {
			defer c.timerWg.Done()
			c.mutex.Lock()
			delete(c.timers, timer)
			c.mutex.Unlock()
			_ = sendMessage(ch, msg, done, true, 0)
		})
		c.timers[timer] = struct{}{}
		c.mutex.Unlock()
		return nil
	}
	c.mutex.Unlock()
	return sendMessage(ch, msg, done, wait, timeout)
}
func sendMessage(ch chan *defaultMessage, msg *defaultMessage, done <-chan struct{}, wait bool, timeout time.Duration) error {
	select {
	case <-done:
		return ErrNotRunning
	default:
	}
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case ch <- msg:
			return nil
		case <-done:
			return ErrNotRunning
		case <-timer.C:
			return ErrQueueFull
		}
	}
	if wait {
		select {
		case ch <- msg:
			return nil
		case <-done:
			return ErrNotRunning
		}
	}
	select {
	case ch <- msg:
		return nil
	case <-done:
		return ErrNotRunning
	default:
		return ErrQueueFull
	}
}
func logError(action, name string, err error) {
	log.Printf("dubi-ex queue: %s %q: %v", action, name, err)
}
