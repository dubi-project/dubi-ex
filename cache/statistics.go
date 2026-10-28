package cache

import "sync/atomic"

type (
	Statistics struct {
		Read   uint64
		Write  uint64
		Delete uint64
		Exists uint64
		Keys   uint64
		Clear  uint64

		Sequence     uint64
		SequenceMany uint64

		Hit   uint64
		Miss  uint64
		Error uint64

		Instances map[string]Statistics
	}
	cacheStats struct {
		read         atomic.Uint64
		write        atomic.Uint64
		delete       atomic.Uint64
		exists       atomic.Uint64
		keys         atomic.Uint64
		clear        atomic.Uint64
		sequence     atomic.Uint64
		sequenceMany atomic.Uint64
		hit          atomic.Uint64
		miss         atomic.Uint64
		error        atomic.Uint64
	}
)

func (m *Module) Stats() Statistics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	out := m.stats.snapshot()
	out.Instances = map[string]Statistics{}
	for name, inst := range m.instances {
		out.Instances[name] = inst.stats.snapshot()
	}
	return out
}

func (m *Module) StatsFrom(conn string) (Statistics, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	inst, ok := m.instances[conn]
	if !ok {
		return Statistics{}, ErrInvalidConnection
	}
	return inst.stats.snapshot(), nil
}

func (m *Module) ResetStats() {
	m.stats.reset()

	m.mutex.RLock()
	defer m.mutex.RUnlock()
	for _, inst := range m.instances {
		inst.stats.reset()
	}
}

func (s *cacheStats) snapshot() Statistics {
	return Statistics{
		Read:         s.read.Load(),
		Write:        s.write.Load(),
		Delete:       s.delete.Load(),
		Exists:       s.exists.Load(),
		Keys:         s.keys.Load(),
		Clear:        s.clear.Load(),
		Sequence:     s.sequence.Load(),
		SequenceMany: s.sequenceMany.Load(),
		Hit:          s.hit.Load(),
		Miss:         s.miss.Load(),
		Error:        s.error.Load(),
	}
}

func (s *cacheStats) reset() {
	s.read.Store(0)
	s.write.Store(0)
	s.delete.Store(0)
	s.exists.Store(0)
	s.keys.Store(0)
	s.clear.Store(0)
	s.sequence.Store(0)
	s.sequenceMany.Store(0)
	s.hit.Store(0)
	s.miss.Store(0)
	s.error.Store(0)
}
