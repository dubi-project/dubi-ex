package cache

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coocood/freecache"
)

type memoryDriver struct{}

type memoryConnection struct {
	mutex sync.RWMutex
	size  int
	cache *freecache.Cache
}

func (*memoryDriver) Connect(inst *Instance) (Connect, error) {
	size := 64 * 1024 * 1024
	if value, ok := inst.Settings["size"]; ok {
		number, err := integer(value)
		if err != nil || number <= 0 || int64(int(number)) != number {
			return nil, fmt.Errorf("cache memory: size must be a positive byte count")
		}
		size = int(number)
	}
	return &memoryConnection{size: size}, nil
}

func (c *memoryConnection) Open() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.cache == nil {
		c.cache = freecache.NewCache(c.size)
	}
	return nil
}

func (c *memoryConnection) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.cache = nil
	return nil
}

func (c *memoryConnection) Read(key string) ([]byte, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.cache == nil {
		return nil, ErrNotOpen
	}
	data, err := c.cache.Get([]byte(key))
	if errors.Is(err, freecache.ErrNotFound) {
		return nil, nil
	}
	if err == nil && data == nil {
		data = []byte{}
	}
	return data, err
}

func (c *memoryConnection) Write(key string, value []byte, expire time.Duration) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.cache == nil {
		return ErrNotOpen
	}
	seconds, err := memoryExpireSeconds(expire)
	if err != nil {
		return err
	}
	return c.cache.Set([]byte(key), value, seconds)
}

func (c *memoryConnection) Exists(key string) (bool, error) {
	data, err := c.Read(key)
	return data != nil, err
}

func (c *memoryConnection) Delete(key string) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.cache == nil {
		return ErrNotOpen
	}
	c.cache.Del([]byte(key))
	return nil
}

func (c *memoryConnection) Sequence(key string, start, step int64, expire time.Duration) (int64, error) {
	values, err := c.SequenceMany(key, start, step, 1, expire)
	if err != nil {
		return -1, err
	}
	return values[0], nil
}

func (c *memoryConnection) SequenceMany(key string, start, step, count int64, expire time.Duration) ([]int64, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.cache == nil {
		return nil, ErrNotOpen
	}
	if count <= 0 {
		return []int64{}, nil
	}
	if count > 1<<20 {
		return nil, ErrInvalidCount
	}
	seconds, err := memoryExpireSeconds(expire)
	if err != nil {
		return nil, err
	}
	var values []int64
	var updateErr error
	_, _, err = c.cache.Update([]byte(key), func(data []byte, found bool) ([]byte, bool, int) {
		current := start
		if found {
			current, updateErr = strconv.ParseInt(string(data), 10, 64)
			if updateErr == nil {
				current, updateErr = addSequence(current, step)
			}
			if updateErr != nil {
				return nil, false, 0
			}
		}
		values = make([]int64, int(count))
		for i := range values {
			if i > 0 {
				current, updateErr = addSequence(current, step)
				if updateErr != nil {
					return nil, false, 0
				}
			}
			values[i] = current
		}
		return []byte(strconv.FormatInt(current, 10)), true, seconds
	})
	if updateErr != nil {
		return nil, updateErr
	}
	if err != nil {
		return nil, err
	}
	return values, nil
}

func addSequence(current, step int64) (int64, error) {
	if (step > 0 && current > math.MaxInt64-step) || (step < 0 && current < math.MinInt64-step) {
		return 0, ErrSequenceOverflow
	}
	return current + step, nil
}

func (c *memoryConnection) Keys(prefix string) ([]string, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	if c.cache == nil {
		return nil, ErrNotOpen
	}
	keys := make([]string, 0)
	iterator := c.cache.NewIterator()
	for entry := iterator.Next(); entry != nil; entry = iterator.Next() {
		if key := string(entry.Key); strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (c *memoryConnection) Clear(prefix string) error {
	// An exclusive lock makes prefix scans and deletion atomic with respect to
	// other operations on this memory connection, including sequences.
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.cache == nil {
		return ErrNotOpen
	}
	if prefix == "" {
		c.cache.Clear()
		return nil
	}
	var keys [][]byte
	iterator := c.cache.NewIterator()
	for entry := iterator.Next(); entry != nil; entry = iterator.Next() {
		if strings.HasPrefix(string(entry.Key), prefix) {
			keys = append(keys, entry.Key)
		}
	}
	for _, key := range keys {
		c.cache.Del(key)
	}
	return nil
}

// freecache has second-resolution expiration; round up positive durations so
// a subsecond duration cannot accidentally become a permanent cache entry.
func memoryExpireSeconds(expire time.Duration) (int, error) {
	if expire <= 0 {
		return 0, nil
	}
	seconds := int64(expire / time.Second)
	if expire%time.Second != 0 {
		seconds++
	}
	if seconds > int64(math.MaxUint32)-time.Now().Unix() {
		return 0, fmt.Errorf("cache memory: expiration out of range")
	}
	return int(seconds), nil
}
