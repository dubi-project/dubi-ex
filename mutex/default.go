package mutex

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type defaultDriver struct{}

type defaultConnect struct {
	mutex    sync.Mutex
	instance *Instance
	locks    map[string]defaultLock
	opened   bool
	closed   bool
	now      func() time.Time
}

type defaultLock struct {
	until time.Time
	token string
}

func (d *defaultDriver) Connect(inst *Instance) (Connection, error) {
	return &defaultConnect{
		instance: inst,
		locks:    make(map[string]defaultLock, 0),
	}, nil
}

func (c *defaultConnect) Open() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.locks == nil {
		c.locks = make(map[string]defaultLock, 0)
	}
	c.opened = true
	c.closed = false
	return nil
}

func (c *defaultConnect) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.locks = make(map[string]defaultLock, 0)
	c.opened = false
	c.closed = true
	return nil
}

func (c *defaultConnect) Lock(key string, expire time.Duration) error {
	_, err := c.LockToken(key, expire)
	return err
}

func (c *defaultConnect) LockToken(key string, expire time.Duration) (string, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err := c.stateError(); err != nil {
		return "", err
	}
	c.cleanupExpiredLocked(c.timeNow())

	if expire < 0 {
		return "", ErrInvalidLease
	}
	if expire <= 0 {
		expire = c.instance.Config.Expire
	}
	if expire <= 0 {
		expire = time.Second
	}

	if current, ok := c.locks[key]; ok {
		if c.timeNow().Before(current.until) {
			return "", ErrLocked
		}
		delete(c.locks, key)
	}

	token, err := randToken()
	if err != nil {
		return "", err
	}
	c.locks[key] = defaultLock{
		until: c.timeNow().Add(expire),
		token: token,
	}
	return token, nil
}

func (c *defaultConnect) Unlock(key string) error {
	return c.UnlockToken(key, "")
}

func (c *defaultConnect) UnlockToken(key, token string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err := c.stateError(); err != nil {
		return err
	}
	c.cleanupExpiredLocked(c.timeNow())

	if c.locks == nil {
		return nil
	}
	if current, ok := c.locks[key]; ok {
		if token != "" && current.token != token {
			return nil
		}
	}
	delete(c.locks, key)
	return nil
}

func (c *defaultConnect) Refresh(key string, expire time.Duration) error {
	return c.RefreshToken(key, "", expire)
}

func (c *defaultConnect) RefreshToken(key, token string, expire time.Duration) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err := c.stateError(); err != nil {
		return err
	}
	now := c.timeNow()
	c.cleanupExpiredLocked(now)

	if expire < 0 {
		return ErrInvalidLease
	}
	if expire <= 0 {
		expire = c.instance.Config.Expire
	}
	if expire <= 0 {
		expire = time.Second
	}

	current, ok := c.locks[key]
	if !ok {
		return ErrLostLock
	}
	if token != "" && current.token != token {
		return ErrLostLock
	}

	current.until = now.Add(expire)
	c.locks[key] = current
	return nil
}

func (c *defaultConnect) Locked(key string) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err := c.stateError(); err != nil {
		return false, err
	}
	c.cleanupExpiredLocked(c.timeNow())

	if c.locks == nil {
		return false, nil
	}
	if current, ok := c.locks[key]; ok {
		if c.timeNow().Before(current.until) {
			return true, nil
		}
		delete(c.locks, key)
	}
	return false, nil
}

func (c *defaultConnect) stateError() error {
	if c.opened {
		return nil
	}
	if c.closed {
		return ErrClosed
	}
	return ErrNotReady
}

func (c *defaultConnect) cleanupExpiredLocked(now time.Time) {
	if c.locks == nil {
		return
	}
	for key, current := range c.locks {
		if !now.Before(current.until) {
			delete(c.locks, key)
		}
	}
}

func randToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func (c *defaultConnect) timeNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
