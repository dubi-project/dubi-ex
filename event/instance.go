package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dubi-project/dubi-ex/internal/support"
	dubi "github.com/dubi-project/dubi-go"
	. "github.com/dubi-project/dubi-go/base"
)

const (
	publishSubjectPrefix   = "publish."
	broadcastSubjectPrefix = "broadcast."
)

type Instance struct {
	Name     string
	Config   Config
	Settings Map
	conn     Connection
	module   *Module
	mutex    sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	tasks    chan func()
	stopping bool
	active   sync.WaitGroup
	workers  sync.WaitGroup
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

// A drain timeout cancels cooperative handlers after the grace period. Stop
// still joins running handlers before releasing their module state.
func (inst *Instance) beginStop() func() {
	inst.mutex.Lock()
	cancel := inst.cancel
	inst.mutex.Unlock()
	if cancel == nil {
		return func() {}
	}
	if inst.Config.DrainTimeout <= 0 {
		cancel()
		return func() {}
	}
	timer := time.AfterFunc(inst.Config.DrainTimeout, cancel)
	return func() { timer.Stop(); cancel() }
}
func (inst *Instance) context() context.Context {
	inst.mutex.Lock()
	defer inst.mutex.Unlock()
	if inst.ctx == nil {
		return context.Background()
	}
	return inst.ctx
}

func (inst *Instance) startWorkers() {
	inst.mutex.Lock()
	defer inst.mutex.Unlock()
	inst.stopping = false
	if inst.Config.Workers <= 0 {
		return
	}
	size := inst.Config.Queue
	if size <= 0 {
		size = inst.Config.Workers * 64
	}
	inst.tasks = make(chan func(), size)
	for i := 0; i < inst.Config.Workers; i++ {
		inst.workers.Add(1)
		go func(tasks <-chan func()) {
			defer inst.workers.Done()
			for next := range tasks {
				next()
			}
		}(inst.tasks)
	}
}

// Submit schedules a driver callback and tracks it until shutdown. Bounded
// workers and overflow policies match infrago; Workers=0 runs asynchronously.
func (inst *Instance) Submit(next func()) {
	if next == nil {
		return
	}
	inst.mutex.Lock()
	if inst.stopping {
		inst.mutex.Unlock()
		return
	}
	inst.active.Add(1)
	tasks := inst.tasks
	ctx := inst.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	policy := inst.Config.QueuePolicy
	inst.mutex.Unlock()
	task := func() { defer inst.active.Done(); defer func() { _ = recover() }(); next() }
	if policy == "sync" {
		task()
		return
	}
	if tasks == nil {
		go task()
		return
	}
	switch policy {
	case "drop":
		select {
		case tasks <- task:
		case <-ctx.Done():
			inst.active.Done()
		default:
			inst.active.Done()
		}
	case "async":
		select {
		case tasks <- task:
		case <-ctx.Done():
			inst.active.Done()
		default:
			go task()
		}
	default:
		select {
		case tasks <- task:
		case <-ctx.Done():
			inst.active.Done()
		}
	}
}

func (inst *Instance) stopWorkers() {
	inst.mutex.Lock()
	inst.stopping = true
	tasks := inst.tasks
	inst.mutex.Unlock()
	// Submit cannot add work after stopping is set. Keep workers alive until
	// accepted work completes, then close the task channel.
	inst.active.Wait()
	if tasks != nil {
		close(tasks)
		inst.workers.Wait()
	}
	inst.mutex.Lock()
	inst.tasks = nil
	inst.mutex.Unlock()
}

func (inst *Instance) Serve(name string, data []byte) {
	data = append([]byte(nil), data...)
	inst.Submit(func() { inst.serving(name, data) })
}
func (inst *Instance) ServeSync(name string, data []byte) bool { return inst.serving(name, data).OK() }

func (inst *Instance) serving(subject string, data []byte) (ret Res) {
	name := subject
	if inst.Config.Prefix != "" {
		if len(name) < len(inst.Config.Prefix) || name[:len(inst.Config.Prefix)] != inst.Config.Prefix {
			return dubi.Fail
		}
		name = name[len(inst.Config.Prefix):]
	}
	for _, prefix := range []string{publishSubjectPrefix, broadcastSubjectPrefix} {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			name = name[len(prefix):]
			break
		}
	}
	meta := dubi.NewMeta().WithContext(inst.context())
	ctx := &Context{inst: inst, Meta: meta, Name: name, Value: Map{}, Args: Map{}, Locals: Map{}, Settings: Map{}}
	if cfg, ok := inst.module.events[name]; ok {
		cfg.Settings = support.CloneMap(cfg.Settings)
		ctx.Config = &cfg
		ctx.Settings = cfg.Settings
	}
	defer func() {
		if value := recover(); value != nil {
			ret = dubi.ErrorResult(fmt.Errorf("event panic: %v", value))
		}
	}()
	var err error
	if inst.Config.External {
		err = dubi.Unmarshal(inst.Config.Codec, data, &ctx.Value)
	} else {
		var env msgEnvelope
		err = dubi.Unmarshal(inst.Config.Codec, data, &env)
		if err == nil && env.Name != "" && env.Name != name {
			err = fmt.Errorf("event envelope name does not match subject")
		}
		if err == nil {
			env.Metadata.Apply(meta)
			if env.Payload != nil {
				ctx.Value = env.Payload
			}
		}
	}
	if err != nil {
		ctx.Error(dubi.ErrorResult(err))
	} else {
		inst.open(ctx)
	}
	return ctx.Result()
}

type msgEnvelope struct {
	Name     string           `json:"name"`
	Metadata support.Metadata `json:"metadata"`
	Payload  Map              `json:"payload"`
}
