# cache

`github.com/dubi-project/dubi-ex/cache` 是 dubi-go 的外部缓存模块。
API 参考 infrago/cache，使用 dubi-go 的 Runtime、Registry、Profile 和 Codec。
导入包即自动挂载模块，默认使用进程内的 Go 缓存，无需 Dubi 服务端或外部服务。

## 使用

```go
import (
    "time"

    dubi "github.com/dubi-project/dubi-go"
    . "github.com/dubi-project/dubi-go/base"
    "github.com/dubi-project/dubi-ex/cache"
)

// Runtime 启动后使用缓存，例如在 Method、Service 或 Web Action 内：
key := cache.Key("user", 123)
err := cache.Write(key, Map{"name": "Ada"}, time.Minute)
value, err := cache.Read(key)
```

常规服务使用 `dubi.Run()` 启动并等待退出信号。短程序或嵌入式使用可以调用
`dubi.DefaultRuntime().Run()`，执行缓存操作后调用 `dubi.End()`。
完整可运行程序见 [examples/cache](../examples/cache/main.go)。

主要 API：

- `Read` / `Write`：Map 编解码，默认走 dubi-go 的 JSON Codec。
- `ReadData` / `WriteData`：原始字节，读写不共享调用方的字节缓冲区。
- `Exists` / `Delete`：存在性检查及删除。
- `Sequence` / `SequenceMany`：原子序列号及批量序列号。
- `Keys` / `Clear`：按前缀枚举或清理所有源，包括只允许显式访问的源。
- `ClearAll`：明确清理所有源在配置 Prefix 下的缓存。
- `Stats` / `StatsFrom` / `ResetStats`：进程内的总计及各源计数。
- `Key` / `KeyWith`：缓存 key 拼接。

显式指定源的 API 与 infrago 一致：`ReadFrom`、`WriteTo`、`ReadDataFrom`、
`WriteDataTo`、`ExistsIn`、`DeleteFrom`、`SequenceOn`、`SequenceManyOn`、
`KeysFrom`、`ClearFrom`、`ClearAllFrom`。

未命中返回 `(nil, nil)`；空字节值仍然存在。未启动或已关闭返回 `ErrNotOpen`；
指定的源不存在、或没有可自动分配的源时返回 `ErrInvalidConnection`。
连接或操作失败会返回错误，不会隐式切换到另一个源。

## 配置

配置文件由 dubi-go 统一读取，支持 JSON、TOML、YAML。

单源：

```toml
[cache]
type = "memory"
prefix = "app:"
expire = "10m"

[cache.settings]
size = 67108864
```

多源推荐显式使用 `sources`：

```toml
[cache.sources.primary]
type = "memory"
weight = 1
prefix = "app:"

[cache.sources.secondary]
type = "memory"
weight = 3
prefix = "app:"

[cache.sources.manual]
type = "memory"
weight = -1
```

也支持 infrago 的 `[cache.primary]`、`[cache.secondary]` 形式。
根节的字段配置名为 `default` 的独立源，不会作为其他源的继承配置。
同名源不可同时在两种形式中重复定义。

| 字段 | 含义 |
| --- | --- |
| `type` | 驱动名称，默认 `memory`，`default` 为同一个内存驱动的别名 |
| `weight` | 默认 `1`；`0` 按 `1` 处理；`-1` 只允许显式访问 |
| `prefix` | 驱动实际使用的 key 前缀，API 枚举时自动剥离 |
| `codec` | Map 编解码器，默认 `json`，可使用 dubi-go 注册的 Codec |
| `expire` / `ttl` | 写入的默认 TTL，支持 duration 字符串或数字秒；`ttl` 优先 |
| `allow_clear_all` | 是否允许无前缀的 `Clear()`，默认 `false` |
| `settings` | 驱动独立参数，例如内存容量 `size`（字节）；兼容 `setting` |

写入时传入 TTL 会覆盖默认 TTL；非正 TTL 表示不过期。
Sequence 不继承写入默认 TTL，未显式传入时不过期。Sequence 的首个值为 `start`，
之后每次增加 `step`；内存驱动的批量分配是原子的，溢出不提交修改。
批量 count 非正数时返回空切片，最大为 1,048,576。

多源自动分配使用带权重的一致性哈希环，按逻辑 key 路由。
相同源名和权重会生成相同的分配，配置的声明顺序不会影响路由。
权重表示大量不同 key 的分配比例，不表示单个热点 key 的请求量比例。
调整源或权重会改变部分 key 的归属，不会自动迁移缓存数据；
所有需要共享分片的应用进程应使用一致的源配置。

代码也可以通过统一 Registry 提供默认配置：

```go
dubi.Register("primary", cache.Config{
    Type:   "memory",
    Weight: 1,
    Prefix: "app:",
})
```

`cache.Config` 和 `cache.Configs` 的 Scope 是 `cache:config`。
文件字段逐项覆盖代码默认值；文件配置不受业务 Profile 筛选。
Go 配置及驱动实例统一使用 `Settings` 字段。配置文件同时出现 `setting` 和
`settings` 时按字段合并，复数键的同名字段优先。

## 内存驱动

默认驱动基于纯 Go 的 [freecache](https://github.com/coocood/freecache)，
每个源独立分配缓存，默认容量 64 MiB，最小容量 512 KiB。
容量耗尽时会淘汰数据，因此它适合缓存，不用于持久化记录。

TTL 精度为秒，正的不足整秒部分向上取整。单个 value 的最大大小受
freecache 限制，约为缓存容量的 1/1024；过大的 key/value 会明确返回错误。
关闭 Runtime 会释放内存引用，重新打开得到空缓存。

`Clear()` 空前缀默认返回 `ErrUnsafeClear`，与 infrago 的行为一致。
使用 `ClearAll()` 或配置 `allow_clear_all=true` 可明确允许清理。
清理范围始终限定在该源配置的 Prefix 内。

## 扩展驱动

驱动子包依赖 `cache` 包并在初始化时注册：

```go
func init() {
    cache.RegisterDriver("redis", &Driver{})
}
```

`Driver.Connect(*cache.Instance)` 为每个源创建连接，连接实现 `cache.Connect`。
接口定义见 [driver.go](driver.go)。驱动可通过 `Instance.Config`、`Instance.Settings`
获取配置；模块统一负责选源、Prefix、Codec、计数和生命周期，驱动负责字节操作。

连接必须支持并发，miss 返回 `(nil, nil)`。
`ManyConnect` 是可选接口：实现后可原子分配整批序列号；未实现时逐个调用
`Sequence`，此时批次之间可能交错。

驱动子包必须编译进应用才可由配置选用；未知驱动会使启动失败。
源会按名字顺序打开，部分打开失败时按相反顺序回收。
Runtime 关闭会等待在途缓存操作结束，再关闭连接。

部分 API 实现移植自 infrago/cache，授权见 [LICENSE.infrago](LICENSE.infrago)。
