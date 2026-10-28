package support

import (
	"time"

	dubi "github.com/dubi-project/dubi-go"
)

// Metadata carries portable request state. Consumers create a new immutable
// request ID and preserve correlation through TraceId instead of rewriting Id.
type Metadata struct {
	TraceId  string `json:"trace_id,omitempty"`
	SpanId   string `json:"span_id,omitempty"`
	Language string `json:"language,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Token    string `json:"token,omitempty"`
}

func Capture(meta *dubi.Meta) Metadata {
	if meta == nil {
		meta = dubi.NewMeta()
	}
	trace := meta.TraceId()
	if trace == "" {
		trace = meta.Id()
	}
	return Metadata{trace, meta.SpanId(), meta.Language(), meta.Timezone().String(), meta.Token()}
}

func (state Metadata) Apply(meta *dubi.Meta) {
	meta.TraceId(state.TraceId)
	meta.SpanId(state.SpanId)
	meta.Language(state.Language)
	if location, err := time.LoadLocation(state.Timezone); err == nil {
		meta.Timezone(location)
	}
	if state.Token != "" {
		_, _ = meta.VerifyToken(state.Token)
	}
}
