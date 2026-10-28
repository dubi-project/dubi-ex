package mutex

import (
	"sync"
	"time"
)

// A locker is bound to the exact source session and ownership token that
// acquired its lease. Keep the handle and defer Unlock rather than unlocking
// by key when ownership matters.
type locker struct {
	instance         *Instance
	conn, key, token string
	expire           time.Duration
	mutex            sync.Mutex
	control          sync.Mutex
	released         bool
	keepAliveStop    chan struct{}
	keepAliveDone    chan struct{}
}

func (l *locker) Unlock() error {
	l.control.Lock()
	defer l.control.Unlock()
	l.stopKeepAlive()
	l.mutex.Lock()
	released := l.released
	l.mutex.Unlock()
	if released {
		return nil
	}
	if err := module.unlock(l.conn, l.key, l.token, l.instance); err != nil {
		return err
	}
	l.mutex.Lock()
	l.released = true
	l.mutex.Unlock()
	return nil
}
func (l *locker) Refresh(expires ...time.Duration) error {
	l.mutex.Lock()
	released, expire := l.released, l.expire
	l.mutex.Unlock()
	if released {
		return ErrLostLock
	}
	if len(expires) > 0 {
		if expires[0] < 0 {
			return ErrInvalidLease
		}
		if expires[0] > 0 {
			expire = expires[0]
		}
	}
	if err := module.refresh(l.conn, l.key, l.token, l.instance, expire); err != nil {
		return err
	}
	l.mutex.Lock()
	l.expire = expire
	l.mutex.Unlock()
	return nil
}
func (l *locker) KeepAlive(intervals ...time.Duration) error {
	l.control.Lock()
	defer l.control.Unlock()
	l.stopKeepAlive()
	if err := l.Refresh(); err != nil {
		return err
	}
	l.mutex.Lock()
	interval := l.expire / 2
	l.mutex.Unlock()
	if len(intervals) > 0 && intervals[0] > 0 {
		interval = intervals[0]
	}
	if interval <= 0 {
		interval = time.Nanosecond
	}
	module.mutex.RLock()
	defer module.mutex.RUnlock()
	inst, err := module.getInstLocked(l.conn, l.key)
	if err != nil {
		return err
	}
	if inst != l.instance {
		return ErrClosed
	}
	stop, done := make(chan struct{}), make(chan struct{})
	l.mutex.Lock()
	l.keepAliveStop, l.keepAliveDone = stop, done
	l.mutex.Unlock()
	inst.keepAlive.Add(1)
	go func() {
		defer inst.keepAlive.Done()
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-inst.ctx.Done():
				return
			case <-ticker.C:
				if err := l.Refresh(); err != nil {
					return
				}
			}
		}
	}()
	return nil
}

// control serializes KeepAlive/Unlock so no renewal goroutine can be replaced
// without stopping and joining the previous one.
func (l *locker) stopKeepAlive() {
	l.mutex.Lock()
	stop, done := l.keepAliveStop, l.keepAliveDone
	l.keepAliveStop, l.keepAliveDone = nil, nil
	l.mutex.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}
