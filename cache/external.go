package cache

import (
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

func Read(key string) (Map, error) {
	return module.Read(key)
}

func ReadFrom(conn, key string) (Map, error) {
	return module.ReadFrom(conn, key)
}

func ReadData(key string) ([]byte, error) {
	return module.ReadData(key)
}

func ReadDataFrom(conn, key string) ([]byte, error) {
	return module.ReadDataFrom(conn, key)
}

func Write(key string, value Map, expires ...time.Duration) error {
	return module.Write(key, value, expires...)
}

func WriteTo(conn, key string, value Map, expires ...time.Duration) error {
	return module.WriteTo(conn, key, value, expires...)
}

func WriteData(key string, data []byte, expires ...time.Duration) error {
	return module.WriteData(key, data, expires...)
}

func WriteDataTo(conn, key string, data []byte, expires ...time.Duration) error {
	return module.WriteDataTo(conn, key, data, expires...)
}

func Delete(key string) error {
	return module.Delete(key)
}

func DeleteFrom(conn, key string) error {
	return module.DeleteFrom(conn, key)
}

func Exists(key string) (bool, error) {
	return module.Exists(key)
}

func ExistsIn(conn, key string) (bool, error) {
	return module.ExistsIn(conn, key)
}

func Sequence(key string, start, step int64, expires ...time.Duration) (int64, error) {
	return module.Sequence(key, start, step, expires...)
}

func SequenceOn(conn, key string, start, step int64, expires ...time.Duration) (int64, error) {
	return module.SequenceOn(conn, key, start, step, expires...)
}

func SequenceMany(key string, start, step, count int64, expires ...time.Duration) ([]int64, error) {
	return module.SequenceMany(key, start, step, count, expires...)
}

func SequenceManyOn(conn, key string, start, step, count int64, expires ...time.Duration) ([]int64, error) {
	return module.SequenceManyOn(conn, key, start, step, count, expires...)
}

func Keys(prefixs ...string) ([]string, error) {
	return module.Keys(prefixs...)
}

func KeysFrom(conn string, prefixs ...string) ([]string, error) {
	return module.KeysFrom(conn, prefixs...)
}

func Clear(prefixs ...string) error {
	return module.Clear(prefixs...)
}

func ClearFrom(conn string, prefixs ...string) error {
	return module.ClearFrom(conn, prefixs...)
}

func ClearAll() error {
	return module.ClearAll()
}

func ClearAllFrom(conn string) error {
	return module.ClearAllFrom(conn)
}

func Stats() Statistics {
	return module.Stats()
}

func StatsFrom(conn string) (Statistics, error) {
	return module.StatsFrom(conn)
}

func ResetStats() {
	module.ResetStats()
}
