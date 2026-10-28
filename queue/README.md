# queue

`github.com/dubi-project/dubi-ex/queue` 移植自 infrago/queue，导入即挂载。
只内置进程内 `memory` 驱动（别名 `default`），支持多 worker、延迟投递、显式重试和死信。

```go
dubi.Register("mail.send", queue.Queue{
    Thread: 4,
    Retry: queue.Retry{time.Second, 5 * time.Second},
    Action: func(ctx *queue.Context) {
        ctx.Invoke("mail.deliver", ctx.Args)
        if ctx.Result().Fail() {
            ctx.Retry()
            return
        }
        ctx.Finish()
    },
})

// Runtime 启动后：
err := queue.Publish("mail.send", Map{"recipient": "a@example.test"})
```

组件支持 Queue/Queues、Declare、Filter、Handler、Config/Configs 及指针注册。
Scope 分别为 `queue:queue`、`queue:declare`、`queue:filter`、`queue:handler`、`queue:config`。
Queues 成员自动加注册名称前缀。Profile 选择代码组件，文件配置直接下发。

Queue 支持 Name、Desc、Alias、Args、Settings、Action/Actions、Connect、Thread、Retry。
Context 嵌入 Meta，支持业务 Invoke、结果、多语言和 Codec；每次消费有独立请求 ID、
Settings 和可取消 Context。Filter/Handler 与 event 相同，按名称排序。

## 投递和重试

- `Publish` / `PublishTo`：立即投递，默认按任务名的一致性哈希选择一个源。
- `DeferredPublish` / `DeferredPublishTo`：指定延迟。
- `PublishWithMeta` / `PublishToWithMeta` 及 `ctx.Publish` / `ctx.PublishTo`：保留调用链 Meta。
- `ctx.Attempts()` 从 1 开始；`ctx.Final()` 表示重试次数已耗尽。
- `ctx.Retry()` 使用 Retry 列表中的下一段延迟；也可 `ctx.Retry(delay)`。
- `ctx.Finish()` 明确结束任务；普通完成默认结束。失败的 Res 本身不会触发重试。
- 回调 panic 会转成失败并尝试重试，受相同次数限制。

Retry 有 N 个延迟时最多执行 N+1 次；空 Retry 列表下显式 Retry 可持续重试。
Settings 中 `dead` / `dead_letter` / `dlq` 可指定死信队列；最终失败会投递原业务数据。
死信队列也需要定义本地 Queue，或由外部驱动消费。编码或投递失败会输出日志。

内存驱动不是持久任务系统。关闭会取消未触发的延迟任务，丢弃未完成投递的缓冲数据，
取消活动 Context 并等待 worker 退出。业务回调应响应取消；不要在回调内同步调用 End。

## 配置

默认无需配置；由 dubi-go 读取 JSON/TOML/YAML，按模块下发：

```toml
[queue]
type = "memory"
codec = "json"
prefix = "app:"

[queue.settings]
buffer = 256
blocking_publish = false
publish_timeout = "250ms"
```

多源使用 `[queue.sources.a]`，也支持 `[queue.a]`。根字段定义独立 default 源。
文件字段覆盖代码 Config；settings 兼容 setting，复数同名参数优先。

- `type`：默认 memory，另有 default 别名；以后可由驱动子包注册更多类型。
- `codec`：默认 json；支持 dubi-go 自定义 Codec。
- `external`：true 时只传业务 Map，默认 false，使用携带 Meta 的信封。
- `prefix`：传输队列名前缀，按原值直接拼接。
- `weight`：默认 1，0 按 1 处理；-1 仅显式投递。
- `Queue.Connect`：空或 `*` 时在各源消费；指定源时固定该源。
- `Queue.Thread`：每个队列的 worker 数，默认 1。
- 内存驱动 `settings.buffer` 默认 256，可用 Queue.Settings.buffer 单独覆盖。
- 缓冲满默认返回 ErrQueueFull；blocking_publish 或正的 publish_timeout 启用等待。

Declare 可以为独立生产者定义参数校验；外部驱动也可直接接收未在本进程注册的队列名。
内存驱动只能向已有本地 Queue 投递，不会跨进程共享数据或消费组。

驱动接口见 [driver.go](driver.go)，子包通过 `queue.RegisterDriver` 注册。
移植授权见 [LICENSE.infrago](LICENSE.infrago)。
