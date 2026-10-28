package mutex

import (
	"errors"
	"strings"
	"time"
)

func (m *Module) pushToken(conn, key, token string, expire, grace time.Duration) {
	if token == "" {
		return
	}
	if expire <= 0 {
		expire = time.Second
	}
	if grace < 0 {
		grace = 0
	}
	queueKey := tokenQueueKey(conn, key)
	m.token.Lock()
	defer m.token.Unlock()
	now := time.Now()
	queue := m.compactQueue(m.tokens[queueKey], now)
	queue = append(queue, tokenEntry{token: token, until: now.Add(expire), grace: grace})
	m.tokens[queueKey] = queue
}

func (m *Module) peekToken(conn, key string) (string, bool) {
	queueKey := tokenQueueKey(conn, key)
	m.token.Lock()
	defer m.token.Unlock()
	now := time.Now()
	queue := m.compactQueue(m.tokens[queueKey], now)
	if len(queue) == 0 {
		delete(m.tokens, queueKey)
		return "", false
	}
	m.tokens[queueKey] = queue
	return queue[0].token, true
}

func (m *Module) touchToken(conn, key, token string, expire, grace time.Duration) {
	if token == "" {
		return
	}
	if expire <= 0 {
		expire = time.Second
	}
	if grace < 0 {
		grace = 0
	}
	queueKey := tokenQueueKey(conn, key)
	m.token.Lock()
	defer m.token.Unlock()
	now := time.Now()
	queue := m.compactQueue(m.tokens[queueKey], now)
	for i := range queue {
		if queue[i].token == token {
			queue[i].until = now.Add(expire)
			queue[i].grace = grace
			m.tokens[queueKey] = queue
			return
		}
	}
	if len(queue) == 0 {
		delete(m.tokens, queueKey)
		return
	}
	m.tokens[queueKey] = queue
}

func (m *Module) shiftToken(conn, key, token string) {
	queueKey := tokenQueueKey(conn, key)
	m.token.Lock()
	defer m.token.Unlock()
	queue := m.compactQueue(m.tokens[queueKey], time.Now())
	if len(queue) == 0 {
		delete(m.tokens, queueKey)
		return
	}
	if queue[0].token != token {
		for i, item := range queue {
			if item.token == token {
				queue = append(queue[:i], queue[i+1:]...)
				if len(queue) == 0 {
					delete(m.tokens, queueKey)
				} else {
					m.tokens[queueKey] = queue
				}
				return
			}
		}
		return
	}
	queue = queue[1:]
	if len(queue) == 0 {
		delete(m.tokens, queueKey)
	} else {
		m.tokens[queueKey] = queue
	}
}

func (m *Module) removeToken(conn, key, token string) {
	queueKey := tokenQueueKey(conn, key)
	m.token.Lock()
	defer m.token.Unlock()
	queue := m.compactQueue(m.tokens[queueKey], time.Now())
	for i, item := range queue {
		if item.token == token {
			queue = append(queue[:i], queue[i+1:]...)
			if len(queue) == 0 {
				delete(m.tokens, queueKey)
			} else {
				m.tokens[queueKey] = queue
			}
			return
		}
	}
}

func (m *Module) cleanupExpiredTokens() int {
	m.token.Lock()
	removedByConn := m.cleanupExpiredTokensLocked(time.Now())
	m.token.Unlock()

	removed := 0
	for conn, count := range removedByConn {
		removed += count
		if conn != "" {
			if inst := m.instanceByName(conn); inst != nil {
				inst.stats.cleanup.Add(uint64(count))
			}
		}
	}
	if removed > 0 {
		m.stats.cleanup.Add(uint64(removed))
	}
	return removed
}

func (m *Module) cleanupExpiredTokensLocked(now time.Time) map[string]int {
	removed := map[string]int{}
	for key, queue := range m.tokens {
		compact := m.compactQueue(queue, now)
		diff := len(queue) - len(compact)
		if diff > 0 {
			removed[tokenQueueConn(key)] += diff
		}
		if len(compact) == 0 {
			delete(m.tokens, key)
		} else {
			m.tokens[key] = compact
		}
	}
	return removed
}

func (m *Module) compactQueue(queue []tokenEntry, now time.Time) []tokenEntry {
	if len(queue) == 0 {
		return queue
	}
	out := queue[:0]
	for _, item := range queue {
		if item.until.Add(item.grace).After(now) {
			out = append(out, item)
		}
	}
	return out
}

func (m *Module) activeTokens(conn string) int {
	m.token.Lock()
	defer m.token.Unlock()
	now := time.Now()
	count := 0
	for key, queue := range m.tokens {
		compact := m.compactQueue(queue, now)
		if len(compact) == 0 {
			delete(m.tokens, key)
			continue
		}
		m.tokens[key] = compact
		if conn == "" || tokenQueueConn(key) == conn {
			for _, item := range compact {
				if item.until.After(now) {
					count++
				}
			}
		}
	}
	return count
}

func (m *Module) recordLock(inst *Instance) {
	m.stats.lock.Add(1)
	if inst != nil {
		inst.stats.lock.Add(1)
	}
}

func (m *Module) recordLockError(inst *Instance, key string, err error) {
	if errors.Is(err, ErrLocked) {
		prefix := contentionPrefix(key)
		m.stats.contention.Add(1)
		m.stats.recordContention(key, prefix)
		if inst != nil {
			inst.stats.contention.Add(1)
			inst.stats.recordContention(key, prefix)
		}
		return
	}
	m.recordError(inst)
}

func (m *Module) recordUnlock(inst *Instance) {
	m.stats.unlock.Add(1)
	if inst != nil {
		inst.stats.unlock.Add(1)
	}
}

func (m *Module) recordRefresh(inst *Instance) {
	m.stats.refresh.Add(1)
	if inst != nil {
		inst.stats.refresh.Add(1)
	}
}

func (m *Module) recordCheck(inst *Instance) {
	m.stats.check.Add(1)
	if inst != nil {
		inst.stats.check.Add(1)
	}
}

func (m *Module) recordError(inst *Instance) {
	m.stats.error.Add(1)
	if inst != nil {
		inst.stats.error.Add(1)
	}
}

func (m *Module) instanceByName(name string) *Instance {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.instances[name]
}

func tokenQueueConn(queueKey string) string {
	for i := 0; i < len(queueKey); i++ {
		if queueKey[i] == 0 {
			return queueKey[:i]
		}
	}
	return ""
}

func isLockedError(err error) bool {
	return errors.Is(err, ErrLocked)
}

func contentionPrefix(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	idx := -1
	for _, sep := range []string{":", "-"} {
		if i := strings.Index(key, sep); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
	}
	if idx <= 0 {
		return key
	}
	return key[:idx]
}

func (m *Module) resolveLease(inst *Instance, expires ...time.Duration) (time.Duration, error) {
	if len(expires) > 0 {
		if expires[0] < 0 {
			return 0, ErrInvalidLease
		}
		if expires[0] > 0 {
			return expires[0], nil
		}
	}
	if inst == nil {
		return 0, ErrInvalidLease
	}
	if inst.Config.Expire <= 0 {
		return 0, ErrInvalidLease
	}
	return inst.Config.Expire, nil
}
