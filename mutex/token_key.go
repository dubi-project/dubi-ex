package mutex

import "time"

func tokenQueueKey(conn, key string) string {
	return conn + "\x00" + key
}

func tokenQueueKeyName(queueKey string) string {
	for i := 0; i < len(queueKey); i++ {
		if queueKey[i] == 0 {
			if i+1 >= len(queueKey) {
				return ""
			}
			return queueKey[i+1:]
		}
	}
	return queueKey
}

func durationRemaining(now, until time.Time) time.Duration {
	if !until.After(now) {
		return 0
	}
	return until.Sub(now)
}
