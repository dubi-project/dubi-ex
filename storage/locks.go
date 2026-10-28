package storage

import "sync"

type objectLock struct {
	mu   sync.RWMutex
	refs int
}
type objectLocks struct {
	mu    sync.Mutex
	items map[string]*objectLock
}

func (l *objectLocks) lock(key string, write bool) func() {
	l.mu.Lock()
	if l.items == nil {
		l.items = map[string]*objectLock{}
	}
	entry := l.items[key]
	if entry == nil {
		entry = &objectLock{}
		l.items[key] = entry
	}
	entry.refs++
	l.mu.Unlock()
	if write {
		entry.mu.Lock()
	} else {
		entry.mu.RLock()
	}
	return func() {
		if write {
			entry.mu.Unlock()
		} else {
			entry.mu.RUnlock()
		}
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.items, key)
		}
		l.mu.Unlock()
	}
}
