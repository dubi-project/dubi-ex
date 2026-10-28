package mutex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

func Key(args ...Any) string { return KeyWith("-", args...) }
func KeyWith(sep string, args ...Any) string {
	if sep == "" {
		sep = "-"
	}
	keys := make([]string, 0, len(args))
	for _, arg := range args {
		keys = append(keys, fmt.Sprint(arg))
	}
	return strings.Join(keys, sep)
}
func splitArgs(args ...Any) ([]Any, []time.Duration) {
	keys := make([]Any, 0, len(args))
	var expires []time.Duration
	for _, arg := range args {
		if exp, ok := arg.(time.Duration); ok {
			expires = append(expires, exp)
		} else {
			keys = append(keys, arg)
		}
	}
	return keys, expires
}

func LockOn(source string, args ...Any) (*locker, error) {
	keys, expires := splitArgs(args...)
	key := Key(keys...)
	inst, token, expire, err := module.acquire(source, key, expires...)
	if err != nil {
		return nil, err
	}
	return &locker{instance: inst, conn: inst.Name, key: key, token: token, expire: expire}, nil
}
func Lock(args ...Any) (*locker, error)                     { return LockOn("", args...) }
func TryLockOn(source string, args ...Any) (*locker, error) { return LockOn(source, args...) }
func TryLock(args ...Any) (*locker, error)                  { return Lock(args...) }
func UnlockOn(source string, args ...Any) error {
	keys, _ := splitArgs(args...)
	return module.UnlockOn(source, Key(keys...))
}
func Unlock(args ...Any) error { return UnlockOn("", args...) }
func RefreshOn(source string, args ...Any) error {
	keys, expires := splitArgs(args...)
	return module.RefreshOn(source, Key(keys...), expires...)
}
func Refresh(args ...Any) error { return RefreshOn("", args...) }
func CheckOn(source string, args ...Any) (bool, error) {
	keys, _ := splitArgs(args...)
	return module.LockedOn(source, Key(keys...))
}
func Check(args ...Any) (bool, error) { return CheckOn("", args...) }
func LockedOn(source string, args ...Any) bool {
	locked, err := CheckOn(source, args...)
	return err != nil || locked
}
func Locked(args ...Any) bool { return LockedOn("", args...) }

func WaitLockOn(source string, timeout, interval time.Duration, args ...Any) (*locker, error) {
	if timeout <= 0 {
		timeout = time.Nanosecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	locker, err := WaitLockOnContext(source, ctx, interval, args...)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, ErrTimeout
	}
	return locker, err
}
func WaitLock(timeout, interval time.Duration, args ...Any) (*locker, error) {
	return WaitLockOn("", timeout, interval, args...)
}
func WaitLockContext(ctx context.Context, interval time.Duration, args ...Any) (*locker, error) {
	return WaitLockOnContext("", ctx, interval, args...)
}
func WaitLockOnContext(source string, ctx context.Context, interval time.Duration, args ...Any) (*locker, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	module.mutex.RLock()
	session := module.ctx
	module.mutex.RUnlock()
	if session == nil {
		session = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if session.Err() != nil {
			return nil, ErrClosed
		}
		locker, err := TryLockOn(source, args...)
		if err == nil {
			if ctx.Err() != nil {
				_ = locker.Unlock()
				return nil, ctx.Err()
			}
			return locker, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-session.Done():
			timer.Stop()
			return nil, ErrClosed
		case <-timer.C:
		}
	}
}

func Stats() Statistics                                { return module.Stats() }
func StatsFrom(source string) (Statistics, error)      { return module.StatsFrom(source) }
func ResetStats()                                      { module.ResetStats() }
func Capabilities() map[string]Capability              { return module.Capabilities() }
func CapabilityFrom(source string) (Capability, error) { return module.CapabilityFrom(source) }
func Debug() DebugInfo                                 { return module.Debug() }
func DebugTokens() []DebugToken                        { return module.DebugTokens() }
