package event

import (
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func Publish(name string, values ...Map) error {
	return module.publish("", name, values...)
}

func PublishTo(conn, name string, values ...Map) error {
	return module.publish(conn, name, values...)
}

func PublishWithMeta(meta *dubi.Meta, name string, values ...Map) error {
	return module.publishMeta(meta, "", name, values...)
}

func PublishToWithMeta(meta *dubi.Meta, conn, name string, values ...Map) error {
	return module.publishMeta(meta, conn, name, values...)
}

func Broadcast(name string, values ...Map) error {
	return module.broadcast("", name, values...)
}

func BroadcastTo(conn, name string, values ...Map) error {
	return module.broadcast(conn, name, values...)
}

func BroadcastWithMeta(meta *dubi.Meta, name string, values ...Map) error {
	return module.broadcastMeta(meta, "", name, values...)
}

func BroadcastToWithMeta(meta *dubi.Meta, conn, name string, values ...Map) error {
	return module.broadcastMeta(meta, conn, name, values...)
}
