package queue

import (
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

func (m *Module) publish(source, name string, value Map, delays ...time.Duration) error {
	return m.publishMeta(nil, source, name, value, delays...)
}
func (m *Module) publishMeta(meta *dubi.Meta, source, name string, value Map, delays ...time.Duration) error {
	if err := validateName(name); err != nil {
		return err
	}
	m.mutex.RLock()
	if !m.opened || m.stopping {
		m.mutex.RUnlock()
		return ErrNotRunning
	}
	cfg := m.queues[name]
	dec := m.declares[name]
	if source == "" {
		if cfg.Connect != "" && cfg.Connect != "*" {
			source = cfg.Connect
		} else {
			source = m.hashring.Locate(name)
		}
	}
	inst := m.instances[source]
	if inst == nil || (cfg.Connect != "" && cfg.Connect != "*" && cfg.Connect != source) {
		m.mutex.RUnlock()
		return ErrNoConnection
	}
	m.mutex.RUnlock()
	if value == nil {
		value = Map{}
	}
	if len(dec.Args) > 0 {
		mapped, res := support.Mapping(meta, dec.Args, value, dec.Nullable)
		if res != nil && res.Fail() {
			return &PayloadError{Queue: name, Result: res}
		}
		value = mapped
	}
	var body any = value
	if !inst.Config.External {
		body = msgEnvelope{Name: name, Metadata: support.Capture(meta), Payload: value}
	}
	data, err := dubi.Marshal(inst.Config.Codec, body)
	if err != nil {
		return err
	}
	if len(delays) > 0 {
		return inst.conn.DeferredPublish(inst.Config.Prefix+name, data, delays[0])
	}
	return inst.conn.Publish(inst.Config.Prefix+name, data)
}

func PublishWithMeta(meta *dubi.Meta, name string, values ...Map) error {
	var value Map
	if len(values) > 0 {
		value = values[0]
	}
	return module.publishMeta(meta, "", name, value)
}
func PublishToWithMeta(meta *dubi.Meta, source, name string, values ...Map) error {
	var value Map
	if len(values) > 0 {
		value = values[0]
	}
	return module.publishMeta(meta, source, name, value)
}
func (ctx *Context) Publish(name string, values ...Map) error {
	return PublishWithMeta(ctx.Meta, name, values...)
}
func (ctx *Context) PublishTo(source, name string, values ...Map) error {
	return PublishToWithMeta(ctx.Meta, source, name, values...)
}
