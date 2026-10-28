package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
)

const virtualNodes = 128
const maxRingPoints = 1 << 20

type ringPoint struct {
	hash   uint64
	source string
}
type hashRing struct{ points []ringPoint }

// Source names and virtual-node positions are stable across map iteration and
// membership changes. Adding a source does not regenerate existing positions.
func newHashRing(weights map[string]int) (*hashRing, error) {
	total := 0
	for _, weight := range weights {
		if weight < 0 || weight > (maxRingPoints-total)/virtualNodes {
			return nil, fmt.Errorf("cache: hash ring exceeds %d points", maxRingPoints)
		}
		total += weight * virtualNodes
	}
	ring := &hashRing{points: make([]ringPoint, 0, total)}
	for _, name := range sortedKeys(weights) {
		for i := 0; i < weights[name]*virtualNodes; i++ {
			ring.points = append(ring.points, ringPoint{hash: hash(name + ":" + strconv.Itoa(i)), source: name})
		}
	}
	sort.Slice(ring.points, func(i, j int) bool {
		if ring.points[i].hash == ring.points[j].hash {
			return ring.points[i].source < ring.points[j].source
		}
		return ring.points[i].hash < ring.points[j].hash
	})
	return ring, nil
}

func hash(value string) uint64 {
	bytes := sha256.Sum256([]byte(value))
	return binary.BigEndian.Uint64(bytes[:8])
}

func (h *hashRing) Locate(key string) string {
	if h == nil || len(h.points) == 0 {
		return ""
	}
	value := hash(key)
	i := sort.Search(len(h.points), func(i int) bool { return h.points[i].hash >= value })
	if i == len(h.points) {
		i = 0
	}
	return h.points[i].source
}
