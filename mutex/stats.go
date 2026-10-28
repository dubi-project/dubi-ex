package mutex

import (
	"sort"
	"sync"
	"sync/atomic"
)

type (
	ContentionStat struct {
		Name  string
		Count uint64
	}

	Statistics struct {
		Lock               uint64
		Unlock             uint64
		Refresh            uint64
		Check              uint64
		Contention         uint64
		Error              uint64
		Cleanup            uint64
		ActiveTokens       int
		ContentionByKey    map[string]uint64
		ContentionByPrefix map[string]uint64
		HotKeys            []ContentionStat
		Instances          map[string]Statistics
	}

	mutexStats struct {
		lock       atomic.Uint64
		unlock     atomic.Uint64
		refresh    atomic.Uint64
		check      atomic.Uint64
		contention atomic.Uint64
		error      atomic.Uint64
		cleanup    atomic.Uint64

		mutex              sync.Mutex
		contentionByKey    map[string]uint64
		contentionByPrefix map[string]uint64
	}
)

func (s *mutexStats) snapshot(active int) Statistics {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	byKey := cloneUint64Map(s.contentionByKey)
	byPrefix := cloneUint64Map(s.contentionByPrefix)
	return Statistics{
		Lock:               s.lock.Load(),
		Unlock:             s.unlock.Load(),
		Refresh:            s.refresh.Load(),
		Check:              s.check.Load(),
		Contention:         s.contention.Load(),
		Error:              s.error.Load(),
		Cleanup:            s.cleanup.Load(),
		ActiveTokens:       active,
		ContentionByKey:    byKey,
		ContentionByPrefix: byPrefix,
		HotKeys:            hotContentions(byKey, 10),
	}
}

func (s *mutexStats) reset() {
	s.lock.Store(0)
	s.unlock.Store(0)
	s.refresh.Store(0)
	s.check.Store(0)
	s.contention.Store(0)
	s.error.Store(0)
	s.cleanup.Store(0)

	s.mutex.Lock()
	s.contentionByKey = nil
	s.contentionByPrefix = nil
	s.mutex.Unlock()
}

func (s *mutexStats) recordContention(key, prefix string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if key != "" {
		if s.contentionByKey == nil {
			s.contentionByKey = map[string]uint64{}
		}
		s.contentionByKey[key]++
	}
	if prefix != "" {
		if s.contentionByPrefix == nil {
			s.contentionByPrefix = map[string]uint64{}
		}
		s.contentionByPrefix[prefix]++
	}
}

func cloneUint64Map(src map[string]uint64) map[string]uint64 {
	if len(src) == 0 {
		return map[string]uint64{}
	}
	dst := make(map[string]uint64, len(src))
	for key, val := range src {
		dst[key] = val
	}
	return dst
}

func hotContentions(src map[string]uint64, limit int) []ContentionStat {
	if len(src) == 0 || limit <= 0 {
		return []ContentionStat{}
	}
	items := make([]ContentionStat, 0, len(src))
	for key, val := range src {
		items = append(items, ContentionStat{Name: key, Count: val})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Name < items[j].Name
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
