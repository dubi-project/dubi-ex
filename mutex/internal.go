package mutex

import (
	"errors"
	"time"
)

// Callers hold m.mutex.RLock through the backend operation so Close cannot
// close a connection between source selection and the lock operation.
func (m *Module) getInstLocked(source, key string) (*Instance, error) {
	if !m.opened {
		if m.closed {
			return nil, ErrClosed
		}
		return nil, ErrNotReady
	}
	if source == "" {
		source = m.ring.Locate(key)
	}
	inst := m.instances[source]
	if inst == nil {
		return nil, ErrInvalidConnection
	}
	return inst, nil
}

func (m *Module) acquire(source, key string, expires ...time.Duration) (*Instance, string, time.Duration, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	inst, err := m.getInstLocked(source, key)
	if err != nil {
		m.recordError(nil)
		return nil, "", 0, err
	}
	expire, err := m.resolveLease(inst, expires...)
	if err != nil {
		m.recordError(inst)
		return nil, "", 0, err
	}
	realKey := inst.Config.Prefix + key
	var token string
	if backend, ok := inst.conn.(TokenConnection); ok {
		token, err = backend.LockToken(realKey, expire)
		if err == nil && token == "" {
			err = ErrTokenRequired
		}
		if err == nil {
			m.pushToken(inst.Name, realKey, token, expire, inst.Config.TokenGrace)
		}
	} else {
		err = inst.conn.Lock(realKey, expire)
	}
	if err != nil {
		m.recordLockError(inst, key, err)
		return nil, "", 0, err
	}
	m.recordLock(inst)
	return inst, token, expire, nil
}

func (m *Module) LockOn(source, key string, expires ...time.Duration) error {
	_, _, _, err := m.acquire(source, key, expires...)
	return err
}
func (m *Module) Lock(key string, expires ...time.Duration) error {
	return m.LockOn("", key, expires...)
}
func (m *Module) UnlockOn(source, key string) error { return m.unlock(source, key, "", nil) }
func (m *Module) Unlock(key string) error           { return m.UnlockOn("", key) }
func (m *Module) RefreshOn(source, key string, expires ...time.Duration) error {
	return m.refresh(source, key, "", nil, expires...)
}
func (m *Module) Refresh(key string, expires ...time.Duration) error {
	return m.RefreshOn("", key, expires...)
}

func (m *Module) unlock(source, key, token string, expected *Instance) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	inst, err := m.getInstLocked(source, key)
	if err != nil {
		m.recordError(nil)
		return err
	}
	if expected != nil && inst != expected {
		return ErrClosed
	}
	realKey := inst.Config.Prefix + key
	if backend, ok := inst.conn.(TokenConnection); ok {
		explicit := token != ""
		if !explicit {
			var found bool
			token, found = m.peekToken(inst.Name, realKey)
			if !found {
				m.recordError(inst)
				return ErrTokenRequired
			}
		}
		err = backend.UnlockToken(realKey, token)
		if err == nil {
			if explicit {
				m.removeToken(inst.Name, realKey, token)
			} else {
				m.shiftToken(inst.Name, realKey, token)
			}
		}
	} else {
		err = inst.conn.Unlock(realKey)
	}
	if err != nil {
		m.recordError(inst)
	} else {
		m.recordUnlock(inst)
	}
	return err
}

func (m *Module) refresh(source, key, token string, expected *Instance, expires ...time.Duration) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	inst, err := m.getInstLocked(source, key)
	if err != nil {
		m.recordError(nil)
		return err
	}
	if expected != nil && inst != expected {
		return ErrClosed
	}
	expire, err := m.resolveLease(inst, expires...)
	if err != nil {
		m.recordError(inst)
		return err
	}
	realKey := inst.Config.Prefix + key
	if backend, ok := inst.conn.(TokenRefresher); ok {
		if token == "" {
			var found bool
			token, found = m.peekToken(inst.Name, realKey)
			if !found {
				m.recordError(inst)
				return ErrTokenRequired
			}
		}
		err = backend.RefreshToken(realKey, token, expire)
		if err == nil {
			m.touchToken(inst.Name, realKey, token, expire, inst.Config.TokenGrace)
		} else if errors.Is(err, ErrLostLock) {
			m.removeToken(inst.Name, realKey, token)
		}
	} else if backend, ok := inst.conn.(Refresher); ok {
		if token != "" {
			err = ErrUnsupportedRefresh
		} else {
			err = backend.Refresh(realKey, expire)
		}
	} else {
		err = ErrUnsupportedRefresh
	}
	if err != nil {
		m.recordError(inst)
	} else {
		m.recordRefresh(inst)
	}
	return err
}

func (m *Module) LockedOn(source, key string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	inst, err := m.getInstLocked(source, key)
	if err != nil {
		m.recordError(nil)
		return false, err
	}
	checker, ok := inst.conn.(Checker)
	if !ok {
		m.recordError(inst)
		return false, ErrUnsupportedCheck
	}
	locked, err := checker.Locked(inst.Config.Prefix + key)
	if err != nil {
		m.recordError(inst)
	} else {
		m.recordCheck(inst)
	}
	return locked, err
}
func (m *Module) Locked(key string) (bool, error) { return m.LockedOn("", key) }
