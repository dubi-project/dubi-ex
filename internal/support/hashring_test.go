package support

import (
	"strconv"
	"testing"
)

func TestHashRingIsStableAndWeighted(t *testing.T) {
	a, err := NewHashRing(map[string]int{"a": 1, "b": 3})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewHashRing(map[string]int{"b": 3, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i := 0; i < 20000; i++ {
		key := strconv.Itoa(i)
		selected := a.Locate(key)
		if selected != b.Locate(key) {
			t.Fatal("routing depends on map iteration")
		}
		counts[selected]++
	}
	if share := float64(counts["b"]) / 20000; share < .65 || share > .85 {
		t.Fatalf("unexpected 1:3 distribution: %v", counts)
	}
	c, err := NewHashRing(map[string]int{"a": 1, "b": 3, "c": 1})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		key := strconv.Itoa(i)
		if before, after := a.Locate(key), c.Locate(key); before != after && after != "c" {
			t.Fatalf("existing sources remapped: %s -> %s", before, after)
		}
	}
	if _, err := NewHashRing(map[string]int{"too-large": maxRingPoints}); err == nil {
		t.Fatal("unbounded virtual-node allocation")
	}
}
