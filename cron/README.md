# cron

`github.com/dubi-project/dubi-ex/cron` 移植自 infrago/cron，导入即挂载到 dubi-go。
只内置进程内的任务存储和锁（type=memory，别名 default），无需外部服务。

```go
dubi.Register("reports.daily", cron.Job{
    Name: "Daily report",
    Target: "reports.generate",
    Schedule: "0 0 2 * * *",
    Payload: Map{"kind": "daily"},
})
```

Target 使用 Meta.Invoke 调用已注册的 Method/Service。Job/Jobs 和指针均可注册，
Scope 为 `cron:job`；Config 的 Scope 为 `cron:config`。Profile 可以筛选任务。

支持标准五段 cron、带秒的六段 cron、`@every 5s`、`@daily` 等 robfig/cron 表达式。
Schedule 和 Schedules 合并去重；无效表达式、缺失 Target 会导致启动或 Add 明确失败。
本地调度使用进程时区；cron 表达式可用 `CRON_TZ=...` 指定时区。
进程停顿后跳过错过的 tick，恢复时不会补发大量历史执行。

## API

- `Add(name, job)`：新增或替换任务，运行中可用。
- `Remove`、`Enable`、`Disable`：管理任务。
- `ListJobs()`：获取任务快照。
- `ListLogs(name, offset, limit)`：总数和倒序执行历史；内存驱动每个任务保留最近 10 次。
- `Ready()`：查询调度状态。

任务执行时复制 Payload，并记录开始、结束、成功状态和错误。
停止时取消活动任务的 Context，等待执行及日志写入结束后关闭存储。
业务回调应响应取消；不要在回调内同步调用全局 End。

## 配置

无需配置即可使用；由 dubi-go 读取 JSON/TOML/YAML 并下发：

```toml
[cron]
type = "memory"
tick = "1s"
sync = "5s"
lock_ttl = "30s"

[cron.settings]
# 驱动专用参数
```

Config 为代码默认值，文件逐字段覆盖。settings 兼容 setting，复数参数优先。
sync 兼容 reload，lock_ttl 兼容 lockttl。数值 duration 按秒解析。

保留驱动扩展接口，子包通过 `cron.RegisterDriver` 注册，接口见 [driver.go](driver.go)。
内存锁只对当前进程有效：多个进程会各自运行同名任务，不能靠它保证集群内只执行一次。
任务定义和日志不持久化，关闭后重新打开默认存储会丢失历史记录。
锁用于同一任务及计划触发点去重，不阻止同一任务的不同触发点并行执行。

移植授权见 [LICENSE.infrago](LICENSE.infrago)。
