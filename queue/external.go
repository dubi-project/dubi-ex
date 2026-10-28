package queue

import (
	"time"

	. "github.com/dubi-project/dubi-go/base"
)

func Publish(name string, values ...Map) error {
	var value Map
	if len(values) > 0 {
		value = values[0]
	}
	return module.publish("", name, value)
}

func PublishTo(conn, name string, values ...Map) error {
	var value Map
	if len(values) > 0 {
		value = values[0]
	}
	return module.publish(conn, name, value)
}

func DeferredPublish(name string, value Map, delay time.Duration) error {
	return module.publish("", name, value, delay)
}

func DeferredPublishTo(conn, name string, value Map, delay time.Duration) error {
	return module.publish(conn, name, value, delay)
}
