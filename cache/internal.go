package cache

import (
	"sort"
	"strings"
	"time"

	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

// getInst is called with mutex held by the complete cache operation. Close
// waits for these operations before closing driver connections.
func (m *Module) getInst(conn, key string) (*Instance, error) {
	if !m.opened {
		return nil, ErrNotOpen
	}
	if conn == "" {
		if m.hashring == nil {
			return nil, ErrInvalidConnection
		}
		conn = m.hashring.Locate(key)
	}
	if inst, ok := m.instances[conn]; ok {
		return inst, nil
	}
	return nil, ErrInvalidConnection
}

func (m *Module) Exists(key string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.exists.Add(1)
	inst, err := m.getInst("", key)
	if err != nil {
		m.addError(nil)
		return false, err
	}
	inst.stats.exists.Add(1)
	realKey := inst.Config.Prefix + key
	ok, err := inst.connect.Exists(realKey)
	if err != nil {
		m.addError(inst)
		return false, err
	}
	if ok {
		m.addHit(inst)
	} else {
		m.addMiss(inst)
	}
	return ok, nil
}

func (m *Module) ExistsIn(conn, key string) (bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.exists.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return false, err
	}
	inst.stats.exists.Add(1)
	realKey := inst.Config.Prefix + key
	ok, err := inst.connect.Exists(realKey)
	if err != nil {
		m.addError(inst)
		return false, err
	}
	if ok {
		m.addHit(inst)
	} else {
		m.addMiss(inst)
	}
	return ok, nil
}

func (m *Module) ReadFrom(conn, key string) (Map, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.read.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return nil, err
	}
	inst.stats.read.Add(1)
	realKey := inst.Config.Prefix + key
	data, err := inst.connect.Read(realKey)
	if err != nil {
		m.addError(inst)
		return nil, err
	}
	if data == nil {
		m.addMiss(inst)
		return nil, nil
	}
	val := Map{}
	if err := cacheUnmarshal(inst.Config.Codec, data, &val); err != nil {
		m.addError(inst)
		return nil, err
	}
	m.addHit(inst)
	return val, nil
}

func (m *Module) Read(key string) (Map, error) {
	return m.ReadFrom("", key)
}

func (m *Module) ReadDataFrom(conn, key string) ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.read.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return nil, err
	}
	inst.stats.read.Add(1)
	realKey := inst.Config.Prefix + key
	data, err := inst.connect.Read(realKey)
	if err != nil {
		m.addError(inst)
		return nil, err
	}
	if data == nil {
		m.addMiss(inst)
	} else {
		m.addHit(inst)
	}
	return data, nil
}

func (m *Module) ReadData(key string) ([]byte, error) {
	return m.ReadDataFrom("", key)
}

func (m *Module) WriteTo(conn string, key string, val Map, expires ...time.Duration) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.write.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return err
	}
	inst.stats.write.Add(1)

	expire := inst.Config.Expire
	if len(expires) > 0 {
		expire = expires[0]
	}

	data, err := cacheMarshal(inst.Config.Codec, &val)
	if err != nil {
		m.addError(inst)
		return err
	}

	realKey := inst.Config.Prefix + key
	if err := inst.connect.Write(realKey, data, expire); err != nil {
		m.addError(inst)
		return err
	}
	return nil
}

func (m *Module) Write(key string, val Map, expires ...time.Duration) error {
	return m.WriteTo("", key, val, expires...)
}

func (m *Module) WriteDataTo(conn, key string, data []byte, expires ...time.Duration) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.write.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return err
	}
	inst.stats.write.Add(1)

	expire := inst.Config.Expire
	if len(expires) > 0 {
		expire = expires[0]
	}

	realKey := inst.Config.Prefix + key
	if err := inst.connect.Write(realKey, data, expire); err != nil {
		m.addError(inst)
		return err
	}
	return nil
}

func (m *Module) WriteData(key string, data []byte, expires ...time.Duration) error {
	return m.WriteDataTo("", key, data, expires...)
}

func (m *Module) DeleteFrom(conn, key string) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.delete.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return err
	}
	inst.stats.delete.Add(1)
	realKey := inst.Config.Prefix + key
	if err := inst.connect.Delete(realKey); err != nil {
		m.addError(inst)
		return err
	}
	return nil
}

func (m *Module) Delete(key string) error {
	return m.DeleteFrom("", key)
}

func (m *Module) SequenceOn(conn, key string, start, step int64, expires ...time.Duration) (int64, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.sequence.Add(1)
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return -1, err
	}
	inst.stats.sequence.Add(1)
	expire := time.Duration(0)
	if len(expires) > 0 {
		expire = expires[0]
	}
	realKey := inst.Config.Prefix + key
	val, err := inst.connect.Sequence(realKey, start, step, expire)
	if err != nil {
		m.addError(inst)
		return -1, err
	}
	return val, nil
}

func (m *Module) Sequence(key string, start, step int64, expires ...time.Duration) (int64, error) {
	return m.SequenceOn("", key, start, step, expires...)
}

func (m *Module) SequenceManyOn(conn, key string, start, step, count int64, expires ...time.Duration) ([]int64, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.sequenceMany.Add(1)
	if count > 1<<20 {
		m.addError(nil)
		return nil, ErrInvalidCount
	}
	if count <= 0 {
		return []int64{}, nil
	}
	inst, err := m.getInst(conn, key)
	if err != nil {
		m.addError(nil)
		return nil, err
	}
	inst.stats.sequenceMany.Add(1)
	expire := time.Duration(0)
	if len(expires) > 0 {
		expire = expires[0]
	}
	realKey := inst.Config.Prefix + key
	if conn, ok := inst.connect.(ManyConnect); ok {
		vals, err := conn.SequenceMany(realKey, start, step, count, expire)
		if err != nil {
			m.addError(inst)
			return nil, err
		}
		return vals, nil
	}

	vals := make([]int64, 0, count)
	for i := int64(0); i < count; i++ {
		val, err := inst.connect.Sequence(realKey, start, step, expire)
		if err != nil {
			m.addError(inst)
			return vals, err
		}
		vals = append(vals, val)
	}
	return vals, nil
}

func (m *Module) SequenceMany(key string, start, step, count int64, expires ...time.Duration) ([]int64, error) {
	return m.SequenceManyOn("", key, start, step, count, expires...)
}

func (m *Module) KeysFrom(conn string, prefixs ...string) ([]string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.keys.Add(1)
	if !m.opened {
		m.addError(nil)
		return nil, ErrNotOpen
	}
	prefix := ""
	if len(prefixs) > 0 {
		prefix = prefixs[0]
	}

	keys := make([]string, 0)

	if conn == "" {
		for _, name := range sortedKeys(m.instances) {
			inst := m.instances[name]
			inst.stats.keys.Add(1)
			realPrefix := inst.Config.Prefix + prefix
			temps, err := inst.connect.Keys(realPrefix)
			if err != nil {
				m.addError(inst)
				return keys, err
			}
			for _, temp := range temps {
				keys = append(keys, strings.TrimPrefix(temp, inst.Config.Prefix))
			}
		}
		sort.Strings(keys)
		return keys, nil
	}

	if inst, ok := m.instances[conn]; ok {
		inst.stats.keys.Add(1)
		realPrefix := inst.Config.Prefix + prefix
		temps, err := inst.connect.Keys(realPrefix)
		if err != nil {
			m.addError(inst)
			return keys, err
		}
		for _, temp := range temps {
			keys = append(keys, strings.TrimPrefix(temp, inst.Config.Prefix))
		}
		sort.Strings(keys)
		return keys, nil
	}

	m.addError(nil)
	return keys, ErrInvalidConnection
}

func (m *Module) Keys(prefixs ...string) ([]string, error) {
	return m.KeysFrom("", prefixs...)
}

func (m *Module) ClearFrom(conn string, prefixs ...string) error {
	return m.clearFrom(conn, false, prefixs...)
}

func (m *Module) ClearAllFrom(conn string) error {
	return m.clearFrom(conn, true)
}

func (m *Module) ClearAll() error {
	return m.clearFrom("", true)
}

func (m *Module) clearFrom(conn string, allowAll bool, prefixs ...string) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	m.stats.clear.Add(1)
	if !m.opened {
		m.addError(nil)
		return ErrNotOpen
	}
	prefix := ""
	if len(prefixs) > 0 {
		prefix = prefixs[0]
	}

	if conn == "" {
		for _, name := range sortedKeys(m.instances) {
			inst := m.instances[name]
			if prefix == "" && !allowAll && !inst.Config.AllowClearAll {
				m.addError(inst)
				return ErrUnsafeClear
			}
		}
		for _, name := range sortedKeys(m.instances) {
			inst := m.instances[name]
			inst.stats.clear.Add(1)
			realPrefix := inst.Config.Prefix + prefix
			if err := inst.connect.Clear(realPrefix); err != nil {
				m.addError(inst)
				return err
			}
		}
		return nil
	}

	if inst, ok := m.instances[conn]; ok {
		if prefix == "" && !allowAll && !inst.Config.AllowClearAll {
			m.addError(inst)
			return ErrUnsafeClear
		}
		inst.stats.clear.Add(1)
		realPrefix := inst.Config.Prefix + prefix
		if err := inst.connect.Clear(realPrefix); err != nil {
			m.addError(inst)
			return err
		}
		return nil
	}

	m.addError(nil)
	return ErrInvalidConnection
}

func (m *Module) Clear(prefixs ...string) error {
	return m.ClearFrom("", prefixs...)
}

func (m *Module) addHit(inst *Instance) {
	m.stats.hit.Add(1)
	if inst != nil {
		inst.stats.hit.Add(1)
	}
}

func (m *Module) addMiss(inst *Instance) {
	m.stats.miss.Add(1)
	if inst != nil {
		inst.stats.miss.Add(1)
	}
}

func (m *Module) addError(inst *Instance) {
	m.stats.error.Add(1)
	if inst != nil {
		inst.stats.error.Add(1)
	}
}

func cacheMarshal(codec string, value Any) ([]byte, error) {
	return dubi.Marshal(codec, value)
}

func cacheUnmarshal(codec string, data []byte, value Any) error {
	return dubi.Unmarshal(codec, data, value)
}
