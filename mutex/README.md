# mutex

`github.com/dubi-project/dubi-ex/mutex` 移植自 infrago/mutex，导入即挂载到 dubi-go。
只内置进程内的 `memory` 驱动（`default` 为别名），无需 Dubi 服务端或外部依赖服务。

```go
lock, err := mutex.Lock("order", orderID, 5*time.Second)
if err != nil {
    return err
}
defer lock.Unlock()

// 执行业务；长任务可以主动续租或启用自动续租。
err = lock.KeepAlive()
```

Runtime 启动后使用，配置由 dubi-go 统一读取并下发。完整程序：

```sh
go run ./examples/mutex
```

## 加锁、等待和续租

- `Lock` / `TryLock`：立即尝试，冲突返回 `ErrLocked`。
- `LockOn` / `TryLockOn`：显式指定连接源。
- `WaitLock(timeout, interval, args...)` / `WaitLockOn`：轮询等待，超时返回 `ErrTimeout`。
- `WaitLockContext(ctx, interval, args...)` / `WaitLockOnContext`：接受外部取消，返回 ctx.Err()。
- `lock.Unlock()`：使用持有者 token 释放；重复成功释放为空操作。
- `lock.Refresh(lease...)`：使用同一个 token 续租，可显式修改后续自动续租使用的租期。
- `lock.KeepAlive(interval...)`：立即检查并续租，然后后台周期续租，默认间隔为租期一半。
- `Check` / `CheckOn`：返回锁状态和错误，不会尝试加锁或释放锁。
- `Locked` / `LockedOn`：仅返回 bool，检查失败时保守返回 true。
- `Key` / `KeyWith`：拼接锁 key，默认分隔符为 `-`。

Key 参数中的 `time.Duration` 用作租期，其他参数用于构造 key。
不传租期或传 0 使用配置默认值；负租期返回 `ErrInvalidLease`。
过期后其他持有者可以获得锁，旧句柄的 Unlock 不会释放新锁，旧句柄 Refresh 返回
`ErrLostLock`。句柄固定所属连接和启动会话，Close/Reset 后不会操作新会话中的锁。

保留 `Unlock(args...)`、`UnlockOn`、`Refresh(args...)`、`RefreshOn` 按 key 的兼容接口。
token 驱动下这些接口使用本进程保存的 token 队列，缺失 token 返回 `ErrTokenRequired`。
过期 token 在 token_grace 窗口内保留，以便延迟的旧 helper 调用消费旧 token。
按 key 的调用无法识别具体业务持有者，建议保留 Lock 返回的句柄并由该句柄释放和续租。

关闭 Runtime 会唤醒等待抢锁的调用，返回 ErrClosed；同时停止并等待自动续租及清理
goroutine，等待在途驱动操作结束后关闭连接。Stop 阶段仍允许其他模块释放锁。

## 多源配置

默认不需要配置。单源示例：

```toml
[mutex]
type = "memory"
prefix = "app:"
expire = "5s"
cleanup_interval = "30s"
token_grace = "1m"

[mutex.settings]
# 预留给所选驱动的独立参数
```

支持 JSON、TOML、YAML，字段名统一使用 Type、Settings；文件兼容 `setting`，
同名参数以 `settings` 优先。Config/Configs 可通过 `dubi.Register` 注册值或指针，
Scope 为 `mutex:config`；代码配置受 Profile 筛选，文件字段逐项覆盖所选代码默认值。

多源推荐 `[mutex.sources.a]`、`[mutex.sources.b]`，也支持 infrago 的 `[mutex.a]` 形式。
根字段定义独立的 default 源，不作为其他源的继承配置。

| 字段 | 含义 |
| --- | --- |
| type | 默认 memory；default 为同一内存驱动的别名 |
| prefix | 驱动 key 前缀，按原值直接拼接 |
| weight | 默认 1，0 按 1 处理；-1 只允许显式选择 |
| expire | 默认租期 1s |
| cleanup_interval | 本地 token 清理周期，默认 30s；多源取最短周期 |
| token_grace | 过期 token 的兼容保留窗口，默认 1m |
| settings | 驱动独立参数，兼容 setting |

自动选源使用按 key 分配的带权一致性哈希；源名和权重相同，分配结果与配置顺序无关。
修改源或权重会改变部分 key 的归属，不会自动迁移现存锁。
每个内存源独立，锁不会跨源、跨进程或跨节点共享。

## 观测和扩展

- `Stats` / `StatsFrom` / `ResetStats`：操作、竞争、错误、清理及活动 token 统计。
- `Capabilities` / `CapabilityFrom`：检查、续租、token 和 token 续租能力。
- `Debug` / `DebugTokens`：配置和租期快照，不导出原始 token。
- `Ready()`：是否已启动。

驱动子包在 init 中调用 `mutex.RegisterDriver(name, driver)`；接口见
[driver.go](driver.go)。默认内存驱动实现所有可选能力。
仅实现基础 Connection 的驱动仍可加锁/解锁，检查和续租会明确返回不支持错误，
其安全性取决于该驱动是否支持 token。驱动操作必须支持并发；TokenConnection 成功
获取锁时必须返回非空 token。模块负责选源、前缀、token 管理、统计和生命周期。

移植授权见 [LICENSE.infrago](LICENSE.infrago)。
