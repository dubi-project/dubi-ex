package queue

import (
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
)

func (inst *Instance) open(ctx *Context) {
	ctx.clear()
	ctx.next(inst.module.serveFilters...)
	ctx.next(inst.serve)
	ctx.Next()
}

func (inst *Instance) serve(ctx *Context) {
	ctx.clear()
	ctx.next(inst.module.requestFilters...)
	ctx.next(inst.request)
	ctx.Next()
	inst.response(ctx)
}

func (inst *Instance) request(ctx *Context) {
	ctx.clear()
	ctx.next(inst.finding)
	ctx.next(inst.authorizing)
	ctx.next(inst.arguing)
	ctx.next(inst.execute)
	ctx.Next()
}

func (inst *Instance) execute(ctx *Context) {
	ctx.clear()
	ctx.next(inst.module.executeFilters...)
	if ctx.Config != nil {
		if len(ctx.Config.Actions) > 0 {
			ctx.next(ctx.Config.Actions...)
		}
		if ctx.Config.Action != nil {
			ctx.next(ctx.Config.Action)
		}
	}
	ctx.Next()
}

func (inst *Instance) response(ctx *Context) {
	ctx.clear()
	ctx.next(inst.module.responseFilters...)
	ctx.Next()
	inst.body(ctx)
}

func (inst *Instance) finding(ctx *Context) {
	if ctx.Config == nil {
		ctx.Found()
		return
	}
	ctx.Next()
}

func (inst *Instance) authorizing(ctx *Context) {
	ctx.Next()
}

func (inst *Instance) arguing(ctx *Context) {
	if ctx.Config != nil && len(ctx.Config.Args) > 0 {
		mapped, res := support.Mapping(ctx.Meta, ctx.Config.Args, ctx.Value, ctx.Config.Nullable)
		if res != nil && res.Fail() {
			ctx.Failed(res)
			return
		}
		ctx.Args = mapped
	} else {
		ctx.Args = support.CloneMap(ctx.Value)
	}
	ctx.Next()
}
func (inst *Instance) found(ctx *Context) {
	ctx.clear()
	if ctx.Config != nil && ctx.Config.Found != nil {
		ctx.next(ctx.Config.Found)
	}
	ctx.next(inst.module.foundHandlers...)
	ctx.Next()
}

func (inst *Instance) error(ctx *Context) {
	ctx.clear()
	if ctx.Config != nil && ctx.Config.Error != nil {
		ctx.next(ctx.Config.Error)
	}
	ctx.next(inst.module.errorHandlers...)
	ctx.Next()
}

func (inst *Instance) failed(ctx *Context) {
	ctx.clear()
	if ctx.Config != nil && ctx.Config.Failed != nil {
		ctx.next(ctx.Config.Failed)
	}
	ctx.next(inst.module.failedHandlers...)
	ctx.Next()
}

func (inst *Instance) denied(ctx *Context) {
	ctx.clear()
	if ctx.Config != nil && ctx.Config.Denied != nil {
		ctx.next(ctx.Config.Denied)
	}
	ctx.next(inst.module.deniedHandlers...)
	ctx.Next()
}

func (inst *Instance) body(ctx *Context) {
	if delay, ok := ctx.Body.(time.Duration); ok {
		ctx.Body = retryBody{delay: delay}
	}
	if ctx.Body == nil {
		ctx.Body = finishBody{}
	}
}
