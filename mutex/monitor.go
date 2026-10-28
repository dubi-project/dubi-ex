package mutex

import (
	"github.com/dubi-project/dubi-ex/internal/support"
	"time"
)

func (m *Module) Stats() Statistics {
	m.cleanupExpiredTokens()

	activeTokens := m.activeTokenCounts()
	out := m.stats.snapshot(activeTokens[""])
	out.Instances = map[string]Statistics{}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	for name, inst := range m.instances {
		out.Instances[name] = inst.stats.snapshot(activeTokens[name])
	}
	return out
}

func (m *Module) StatsFrom(conn string) (Statistics, error) {
	m.cleanupExpiredTokens()
	activeTokens := m.activeTokenCounts()

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	inst, ok := m.instances[conn]
	if !ok {
		return Statistics{}, ErrInvalidConnection
	}
	return inst.stats.snapshot(activeTokens[conn]), nil
}

func (m *Module) ResetStats() {
	m.stats.reset()

	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, inst := range m.instances {
		inst.stats.reset()
	}
}

func (m *Module) Capabilities() map[string]Capability {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	out := make(map[string]Capability, len(m.instances))
	for name, inst := range m.instances {
		out[name] = capabilityOf(inst.conn)
	}
	return out
}

func (m *Module) CapabilityFrom(conn string) (Capability, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	inst, ok := m.instances[conn]
	if !ok {
		return Capability{}, ErrInvalidConnection
	}
	return capabilityOf(inst.conn), nil
}

func (m *Module) Debug() DebugInfo {
	m.cleanupExpiredTokens()
	activeTokens := m.activeTokenCounts()

	info := DebugInfo{
		Tokens:    m.DebugTokens(),
		Stats:     m.stats.snapshot(activeTokens[""]),
		Instances: map[string]DebugInstance{},
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	info.Opened = m.opened
	info.Started = m.started
	info.CleanupInterval = m.cleanupInterval

	for name, inst := range m.instances {
		info.Instances[name] = DebugInstance{
			Name:            name,
			Type:            inst.Config.Type,
			Weight:          inst.Config.Weight,
			Prefix:          inst.Config.Prefix,
			Expire:          inst.Config.Expire,
			CleanupInterval: inst.Config.CleanupInterval,
			TokenGrace:      inst.Config.TokenGrace,
			Settings:        support.CloneMap(inst.Config.Settings),
			Capability:      capabilityOf(inst.conn),
			Stats:           inst.stats.snapshot(activeTokens[name]),
		}
	}
	return info
}

func (m *Module) DebugTokens() []DebugToken {
	m.token.Lock()
	defer m.token.Unlock()

	now := time.Now()
	out := make([]DebugToken, 0, len(m.tokens))
	for queueKey, queue := range m.tokens {
		compact := m.compactQueue(queue, now)
		if len(compact) == 0 {
			delete(m.tokens, queueKey)
			continue
		}
		m.tokens[queueKey] = compact

		item := DebugToken{
			Conn:    tokenQueueConn(queueKey),
			Key:     tokenQueueKeyName(queueKey),
			Count:   len(compact),
			Entries: make([]DebugTokenEntry, 0, len(compact)),
		}
		for _, token := range compact {
			active := token.until.After(now)
			if active {
				item.Active++
			} else {
				item.Expired++
			}
			entry := DebugTokenEntry{
				Active:    active,
				Remaining: durationRemaining(now, token.until),
			}
			if !active {
				entry.GraceRemaining = durationRemaining(now, token.until.Add(token.grace))
			}
			item.Entries = append(item.Entries, entry)
		}
		out = append(out, item)
	}
	return out
}

func (m *Module) activeTokenCounts() map[string]int {
	m.token.Lock()
	defer m.token.Unlock()

	now := time.Now()
	counts := map[string]int{"": 0}
	for key, queue := range m.tokens {
		compact := m.compactQueue(queue, now)
		if len(compact) == 0 {
			delete(m.tokens, key)
			continue
		}
		m.tokens[key] = compact
		conn := tokenQueueConn(key)
		for _, item := range compact {
			if item.until.After(now) {
				counts[""]++
				counts[conn]++
			}
		}
	}
	return counts
}
