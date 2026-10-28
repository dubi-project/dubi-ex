# event

`github.com/dubi-project/dubi-ex/event` 移植自 infrago/event，自动挂载到 dubi-go。
只内置进程内的 `memory` 驱动（`default` 为别名），不需要 Dubi 服务端。

```go
dubi.Register("user.created", event.Event{
    Name: "User created",
    Args: Vars{"id": {Type: "string", Required: true}},
    Action: func(ctx *event.Context) {
        ctx.Invoke("audit.created", ctx.Args)
    },
})

// Runtime 启动后：
err := event.Publish("user.created", Map{"id": "u-1"})
```

组件支持 `Event`、`Events`、`Declare`、`Filter`、`Handler`，均可通过 `dubi.Register`
注册值或指针。各自的 Scope 是 `event:event`、`event:declare`、`event:filter`、
`event:handler`；Config/Configs 是 `event:config`。Events 成员自动加注册名称前缀。
Profile 选择代码组件；文件中的连接配置由 Runtime 直接下发。

Event 支持 Alias、Args、Settings、Action/Actions、Connect 及各结果处理器。
Context 嵌入 Meta，可用 Invoke、Mapping、Result、多语言、Codec；每条消息有独立的
请求 ID，跨消息保留 trace、语言、时区和经验证的 token。每次消费复制 Settings。

Filter 提供 Serve → Request → Execute → Response 阶段；调用 `ctx.Next()` 继续。
Handler 和组件支持 Found（未找到）、Error、Failed、Denied。处理器接收 Res。
过滤器和处理器按注册名称排序。发布和消费都会进行 Args 映射。

## API

- `Publish` / `PublishTo`：普通发布；驱动接入共享消息系统时按 role（回退 profile）分组。
- `Broadcast` / `BroadcastTo`：广播订阅，不使用消费组。
- 对应的 `WithMeta` / `ToWithMeta` 方法传递当前 Meta。
- `ctx.Publish`、`ctx.PublishTo`、`ctx.Broadcast`、`ctx.BroadcastTo` 自动传递 Meta。
- `Ready()` 查询是否运行；`RegisterDriver` 用于以后接入驱动子包。

Publish 和 Broadcast 都按事件名的一致性哈希选择一个源；`To` 显式选源。
Broadcast 指源内广播，不表示遍历所有源。单进程内存驱动下两者都会调用本地 Event。
仅声明的生产者可用 Declare，无本地订阅者的内存发布成功但不执行回调。
未注册/声明的事件、错误参数、无可用源及缓冲区满均明确返回错误。

## 配置

无需配置即可使用默认源。配置由 dubi-go 读取 JSON/TOML/YAML 后下发：

```toml
[event]
type = "memory"
workers = 4
queue = 256
queue_policy = "block"
drain_timeout = "5s"
```

多源使用 `[event.sources.a]`、`[event.sources.b]`，也支持 infrago 的 `[event.a]` 形式。
根字段定义独立的 default 源。代码 Config 为默认值，文件字段逐项覆盖。

| 字段 | 含义 |
| --- | --- |
| type | 默认 memory；只内置 memory/default |
| codec | 默认 json，使用 dubi-go 注册的 Codec |
| external | 默认 false；true 时仅编解码业务 Map，不携带消息信封及 Meta |
| prefix | 传输主题前缀；未带分隔符时自动加点 |
| weight | 默认 1，0 按 1 处理，-1 不参与自动选择 |
| workers | 默认 0，每条消息异步执行；正数启用固定 worker 池 |
| queue | worker 待执行队列容量；默认 workers × 64，同时可设置内存主题缓冲容量 |
| queue_policy | block（默认）、drop、sync、async；后两者分别在提交线程执行、满时额外异步执行 |
| drain_timeout | 停止时给予回调的宽限时间，超时取消 Context；最终仍等待正在执行的回调结束 |
| settings | 驱动参数；兼容 setting，复数键优先；内存 buffer 默认 64 |

`Event.Connect` 为空或 `*` 时在各源订阅；指定源名则固定订阅和默认发布到该源。
内存驱动是进程内的临时发布订阅：不跨进程、不持久化、不重投失败回调。
需要任务重试使用 queue。关闭时停止接收并等待回调结束；回调应响应 Context 取消。

驱动实现见 [driver.go](driver.go)，在子包 init 中调用 `event.RegisterDriver`。
Connection.Stop 必须停止驱动回调并等待其结束；不要在消费回调内同步调用全局 End。
移植授权见 [LICENSE.infrago](LICENSE.infrago)。
