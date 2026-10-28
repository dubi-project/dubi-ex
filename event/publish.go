package event

import (
	"fmt"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

// Aliases preserve infrago's error names.
var (
	ErrInvalidEventPayload = ErrInvalidPayload
	ErrEventRunning        = fmt.Errorf("event: already running")
	ErrEventNotRunning     = ErrNotRunning
	ErrEventQueueFull      = ErrQueueFull
)

func (m *Module) publish(conn, name string, values ...Map) error {
	return m.publishMeta(nil, conn, name, values...)
}
func (m *Module) broadcast(conn, name string, values ...Map) error {
	return m.broadcastMeta(nil, conn, name, values...)
}
func (m *Module) publishMeta(meta *dubi.Meta, conn, name string, values ...Map) error {
	return m.publishModeMeta(meta, conn, publishSubjectPrefix, name, values...)
}
func (m *Module) broadcastMeta(meta *dubi.Meta, conn, name string, values ...Map) error {
	return m.publishModeMeta(meta, conn, broadcastSubjectPrefix, name, values...)
}

func (m *Module) publishModeMeta(meta *dubi.Meta, source, mode, name string, values ...Map) error {
	if err := validateName(name); err != nil {
		return err
	}
	m.mutex.RLock()
	if !m.opened || m.stopping {
		m.mutex.RUnlock()
		return ErrNotRunning
	}
	cfg, registered := m.events[name]
	dec, declared := m.declares[name]
	if !registered && !declared {
		m.mutex.RUnlock()
		return ErrInvalidEvent
	}
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
	var value Map
	if len(values) > 0 {
		value = values[0]
	}
	if value == nil {
		value = Map{}
	}
	if len(dec.Args) > 0 {
		mapped, res := support.Mapping(meta, dec.Args, value, dec.Nullable)
		if res != nil && res.Fail() {
			return &PayloadError{Event: name, Result: res}
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
	return inst.conn.Publish(inst.Config.Prefix+mode+name, data)
}
