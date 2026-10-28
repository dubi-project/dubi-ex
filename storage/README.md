# storage

`github.com/dubi-project/dubi-ex/storage` 参考 infrago/storage，导入即挂载到 dubi-go。
默认使用本地文件驱动 `file`，别名 `local` / `default`。对象保存在启动目录下的
`store/storage/<源名>`，无需 Dubi 服务端；关闭 Runtime 不删除已存储对象。

```go
file, err := storage.Upload("/tmp/photo.png")
if err != nil {
    return err
}
code := file.Code() // 可持久化到业务数据库

body, err := storage.Fetch(code)
if err != nil {
    return err
}
defer body.Close()
```

## 上传与定位

- `Upload(pathOrMap, options...)` / `UploadTo(source, pathOrMap, options...)`：路径或 `Map{"file": path}`。
- `UploadReader(reader, options...)` / `UploadReaderTo(source, reader, options...)`：读取调用方提供的流。
- `UploadOption` 包含 Context、Key、Prefix、Name、Type、Mimetype、Metadata、Tags、Expires。
- Type 为明确指定的扩展名，其次由 Mimetype 推断，再使用 Name/源路径的扩展名。
- 未提供 Key 时先将内容快照到临时文件，用 SHA-256 生成 key 和两层散列目录，结束后清理临时文件。
- 指定 Key 时直接流式上传；按逻辑 Prefix + Key 选择源，更新同名 key 不会因为内容变化而换源。
- 最终路径始终组合连接 Config.Prefix、上传 Prefix、对象名；显式 Key 也保留连接前缀。
- 读者由调用方拥有，UploadReader 不关闭调用方的 Reader；Reader 应能配合传入 Context 取消。

File 提供 Base、Prefix、Key、Type、Size、Version、Code、File、Name、Proxy、Remote 方法。
默认 key 是内容寻址；同内容同类型上传会定位到相同对象。本地写入始终先写同目录临时
文件，成功后原子替换，避免半成品及“源文件就是目标文件”导致的数据丢失。

Code 使用带版本的 `v1.` 引用格式，携带源名、对象位置和上传时的大小、内容摘要；
也能 Decode infrago 的旧五字段 Base64 编码。Code 是对象定位信息，不是鉴权凭据。
修改自定义 Key 的对象后，旧 Code 仍定位该 key 的当前内容，Size/Version 则是原上传
快照；需要当前元数据时用 Stat。读旧 Code 按其中的源定位，不重新经过哈希环。

## 读取、下载和 URL

```go
body, err := storage.Fetch(code, storage.FetchOption{
    Context: ctx,
    Range: &storage.Range{Start: 0, End: 0}, // 只读第一个字节
})

copy, err := storage.Download(code) // 完整本地缓存副本
_, err = storage.Download(code, storage.DownloadOption{Target: "/tmp/export.png"})
original, err := storage.Local(code) // 仅支持此能力的驱动返回原文件路径
url, err := storage.Browse(code)     // 访问 URL；不返回服务器磁盘路径
```

- Fetch 返回 `io.ReadCloser`，调用方应 Close。支持随机访问的驱动可额外返回 RandomAccessStream。
- Range=nil 读取完整对象；Start/End 均包含端点，End=-1 表示到结尾；非法范围明确报错。
- Download 的 Target 有值时总是完整导出到该目标；本地与远端驱动采用一致语义。
- Local 返回的原路径应当视为只读，直接修改会绕过框架的写入及缓存管理。
- 本地 Browse 需要 `settings.base_url` / `url`，否则返回 ErrBrowseUnsupported。
- 本地 Metadata、Tags、Expires 仅作为上传选项传给驱动，不作为文件系统元数据持久化；
  对象服务驱动可以自行实现这些参数。

Fetch、Download、Local、Browse、Stat、Remove 的 Option 均可带 Context。
后台流支持 Context 取消；Runtime.Close 会取消操作、自动关闭未关闭的 Fetch 流，
等待在途操作退出后再关闭驱动。自定义驱动和 Reader 需要响应取消。

## 下载缓存和图片处理

Download、图片 Thumbnail 和 Preview 的缓存由主模块统一管理：

- 下载和图片文件都在写完并关闭后原子发布，失败不会留下可复用的半成品。
- 框架覆盖或删除对象时清理对应下载、缩略图、预览缓存。
- 支持 Stat 且提供版本的驱动，每次命中缓存前核实对象仍存在及版本；可感知外部更新、删除。
- 没有版本查询能力的驱动不复用持久缓存，避免仅凭“文件存在”返回旧内容。
- 同对象变更与缓存生成互斥，防止删除或覆盖后又写回旧缓存。
- 缓存路径由模块管理，返回路径不构成长期文件租约；需要稳定副本时显式提供 Download.Target。
- cache_ttl 控制复用时限，不是磁盘容量淘汰策略；大量缓存可由部署侧按需清理。

```go
thumbnail, err := storage.Thumbnail(code, 320, 0, 0)
preview, err := storage.Preview(code, 1024, 768)
```

内置图片处理器支持常见图片格式，WebP 输入默认输出 JPEG。宽或高为 0 时等比计算，
两者均为 0 时使用源尺寸。position=0 居中，1..8 依次为左上、上、右上、左、右、
左下、下、右下。默认限制源图片 4000 万像素、输出 1600 万像素。

保留 Thumbnailer / Previewer 注册机制，字段统一为 Name、Desc、Alias、Action。
自定义 Action 仍使用 `func(file *File, width, height, position int64) (string, error)`；
`file.Context()` 获取取消信号。自定义处理器自行管理其输出和缓存。
提供 ThumbnailContext、PreviewContext 以传入调用方 Context。

## 配置

```toml
[storage]
type = "file"
prefix = "uploads"

[storage.settings]
root = "store/objects"
base_url = "https://files.example.com"

[storage.options]
download = "store/download"
thumbnail = "store/thumbnail"
preview = "store/preview"
temp = "store/temp"
cache_ttl = "10m"
max_image_pixels = 40000000
max_thumbnail_pixels = 16000000
```

由 dubi-go 读取 JSON/TOML/YAML 后下发。`setting` 兼容 `settings`，复数同名字段优先；
`option` 兼容 `options`，也接受 infrago 的 `[file]` 路径配置，模块 options 优先。
对象根目录和下载/图片缓存目录应分开配置。

多源使用 `[storage.sources.primary]` 等，也支持 `[storage.primary]` 形式。
每个源支持 type、weight、prefix、proxy、remote、settings；根字段是独立 default 源。
weight 默认 1，0 按 1 处理，-1 仅显式选择，使用与其他 dubi-ex 模块相同的一致性哈希。
多个本地源默认具有独立根目录；调整连接配置不会自动搬迁已有对象。

Config / Configs 可通过 dubi.Register 注册值或指针，Scope=`storage:config`。
文件字段逐项覆盖代码默认值。Thumbnailer、Previewer 的 Scope 分别是
`storage:thumbnailer`、`storage:previewer`，代码注册受 Profile 筛选。
内置图片处理器随模块提供，不依赖 Profile 注册。

## 扩展驱动

子包在 init 中调用 `storage.RegisterDriver`，接口见 [driver.go](driver.go)。
必需操作只有 Open、Close、Upload、Fetch、Remove；Stat、Local、Browse、Health
分别通过可选接口提供。模块统一负责选源、前缀、编码、缓存、下载及生命周期。
驱动 Upload 必须消费完整输入并原子提交，返回正确的对象位置和实际大小；Fetch
可直接返回网络流，无需为满足 Seek/ReadAt 而先完整落盘。

当前仅内置本地文件驱动，没有包含 S3/MinIO SDK。默认文件 IO 使用 os.Root 限制
相对路径和符号链接的访问范围。驱动应支持并发调用，流的 Close 应能终止阻塞读取。移植设计授权见 [LICENSE.infrago](LICENSE.infrago)。

可运行示例：`go run ./examples/storage`。
