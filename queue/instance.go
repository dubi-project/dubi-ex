package queue

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

type Instance struct {
	Name     string
	Config   Config
	Settings Map
	conn     Connection
	module   *Module
	mutex    sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

func (inst *Instance) context() context.Context {
	inst.mutex.RLock()
	defer inst.mutex.RUnlock()
	if inst.ctx == nil {
		return context.Background()
	}
	return inst.ctx
}
func (inst *Instance) resetContext() {
	inst.mutex.Lock()
	defer inst.mutex.Unlock()
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.ctx, inst.cancel = context.WithCancel(context.Background())
}
func (inst *Instance) cancelContext() {
	inst.mutex.Lock()
	defer inst.mutex.Unlock()
	if inst.cancel != nil {
		inst.cancel()
	}
}
func (*Instance) startWorkers() {}
func (*Instance) stopWorkers()  {}
func (*Instance) Submit(next func()) {
	if next != nil {
		go next()
	}
}

type msgEnvelope struct {
	Name     string           `json:"name"`
	Metadata support.Metadata `json:"metadata"`
	Payload  Map              `json:"payload"`
}

func (inst *Instance) Serve(req Request) (resp Response) {
	name := req.Name
	if inst.Config.Prefix != "" {
		if !strings.HasPrefix(name, inst.Config.Prefix) {
			return
		}
		name = strings.TrimPrefix(name, inst.Config.Prefix)
	}
	requestCtx, cancel := context.WithCancel(inst.context())
	defer cancel()
	ctx := &Context{inst: inst, Meta: dubi.NewMeta().WithContext(requestCtx), context: requestCtx, cancel: cancel, Name: name, Settings: Map{}, Value: Map{}, Args: Map{}, Locals: Map{}, attempt: req.Attempt}
	if ctx.attempt <= 0 {
		ctx.attempt = 1
	}
	if cfg, ok := inst.module.queues[name]; ok {
		cfg.Settings = support.CloneMap(cfg.Settings)
		ctx.Config = &cfg
		ctx.Settings = cfg.Settings
		ctx.final = queueFinal(len(cfg.Retry), ctx.attempt)
	}
	defer func() {
		if value := recover(); value != nil {
			ctx.Result(dubi.ErrorResult(fmt.Errorf("queue panic: %v", value)))
			ctx.Body = retryBody{}
		}
		retry, delay := inst.responseMeta(ctx)
		resp = Response{Retry: retry, Delay: delay}
		if !retry && shouldDeadLetter(ctx, ctx.Result()) {
			inst.dead(ctx, req.Data)
		}
	}()
	var err error
	if inst.Config.External {
		err = dubi.Unmarshal(inst.Config.Codec, req.Data, &ctx.Value)
	} else {
		var env msgEnvelope
		err = dubi.Unmarshal(inst.Config.Codec, req.Data, &env)
		if err == nil && env.Name != "" && env.Name != name {
			err = fmt.Errorf("queue envelope name does not match subject")
		}
		if err == nil {
			env.Metadata.Apply(ctx.Meta)
			if env.Payload != nil {
				ctx.Value = env.Payload
			}
		}
	}
	if err != nil {
		ctx.Error(dubi.ErrorResult(err))
		ctx.Finish()
		return
	}
	inst.open(ctx)
	return
}

func (inst *Instance) responseMeta(ctx *Context) (bool, time.Duration) {
	if ctx.final {
		return false, 0
	}
	if body, ok := ctx.Body.(retryBody); ok {
		if body.delay > 0 {
			return true, body.delay
		}
		return true, inst.nextRetryDelay(ctx)
	}
	if delay, ok := ctx.Body.(time.Duration); ok {
		return true, delay
	}
	return false, 0
}
func (inst *Instance) nextRetryDelay(ctx *Context) time.Duration {
	if ctx.Config == nil || len(ctx.Config.Retry) == 0 {
		return time.Second
	}
	index := ctx.attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ctx.Config.Retry) {
		index = len(ctx.Config.Retry) - 1
	}
	delay := ctx.Config.Retry[index]
	if delay <= 0 {
		return time.Second
	}
	return delay
}
func queueFinal(retryCount, attempt int) bool {
	if attempt <= 0 {
		attempt = 1
	}
	return retryCount > 0 && attempt > retryCount
}
func deadLetterName(ctx *Context) string {
	if ctx == nil || ctx.Config == nil {
		return ""
	}
	for _, key := range []string{"dead", "dead_letter", "dlq"} {
		if name, ok := ctx.Config.Settings[key].(string); ok && name != "" {
			return name
		}
	}
	return ""
}
func shouldDeadLetter(ctx *Context, res Res) bool {
	if deadLetterName(ctx) == "" {
		return false
	}
	if res != nil && res.Fail() {
		return true
	}
	_, retry := ctx.Body.(retryBody)
	return retry && ctx.final
}
func (inst *Instance) dead(ctx *Context, raw []byte) {
	name := deadLetterName(ctx)
	if name == "" {
		return
	}
	value := ctx.Value
	if len(value) == 0 && len(raw) > 0 {
		value = Map{"raw": raw}
	}
	var body any = value
	if !inst.Config.External {
		body = msgEnvelope{Name: name, Metadata: support.Capture(ctx.Meta), Payload: value}
	}
	data, err := dubi.Marshal(inst.Config.Codec, body)
	if err == nil {
		err = inst.conn.Publish(inst.Config.Prefix+name, data)
	}
	if err != nil {
		logError("dead letter", ctx.Name, err)
	}
}
