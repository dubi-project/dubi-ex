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
- `UploadOption` 包含 Context、Key、KeyMode、Size、Prefix、Name、Type、Mimetype、Metadata、Tags、Expires。
- Type 为明确指定的扩展名，其次由 Mimetype 推断，再使用 Name/源路径的扩展名。
- 先选择连接，再由驱动处理命名。显式 Key 按逻辑 Prefix + Key 选源；没有 Key 时按 Prefix + Name 选源（Name 也为空则只使用 Prefix）。UploadTo / UploadReaderTo 可显式指定连接。
- 默认本地驱动在没有 Key 时使用 HashKey：先将内容快照到临时文件，用完整 SHA-1 摘要生成 key 和两层散列目录，结束后清理临时文件。
- 本地 key 使用无填充的 URL-safe Base64，为 27 个字符；目录取 SHA-1 十六进制摘要的前两位、随后两位，例如 `a9/99`。
- 本地 KeyMode=RandomKey 时生成随机 key 并直接流式上传，不做本地快照；同内容重复上传会生成不同对象。
- 外部驱动自行实现命名和摘要策略，框架不强制计算 SHA-1、不为其上传创建临时文件。KeyMode 由驱动解释，连接 settings.key_mode 只作为本地驱动的默认值。
- 指定 Key 时默认本地驱动直接流式上传，更新同名 key 不会因为内容变化而换源。
- Size=nil 表示长度未知，指向 0 表示空文件。路径上传自动提供长度；流上传可按需指定，实际字节数不符时返回 ErrSizeMismatch，不提交半成品。
- 最终路径始终组合连接 Config.Prefix、上传 Prefix、对象名；显式 Key 也保留连接前缀。
- 读者由调用方拥有，UploadReader 不关闭调用方的 Reader；Reader 应能配合传入 Context 取消。

```go
file, err := storage.UploadReader(reader, storage.UploadOption{
    Name: "photo.png",
    KeyMode: storage.RandomKey, // 本地驱动不需要先写入临时文件
})
```

File 提供 Base、Prefix、Key、Type、Size、Hash、Version、Code、File、Name、Proxy、Remote 方法。
本地默认 key 是内容寻址；同一连接及前缀下，同内容同类型上传会定位到相同对象。
不同文件名可能选到不同连接，因此不保证跨连接去重；需要固定连接时使用 UploadTo。
本地写入始终先写同目录临时
文件，成功后原子替换，避免半成品及“源文件就是目标文件”导致的数据丢失。

Hash 是驱动提供的可选内容摘要，本地驱动返回 SHA-1 十六进制摘要（40 字符）；
Version 是后端提供的不透明版本标记，两者独立。外部驱动的摘要算法由其自行定义，也可不提供 Hash。
驱动没有返回版本时 Version 为空；Stat 的 Hash 也可以为空，本地 Stat 不会为此读取整个文件。
SHA-1 用于普通文件内容寻址，不用于安全校验；它已有已知碰撞攻击，不能据此保证不同内容绝不重名。
原 SHA-256 文件继续按旧 Code 中的 key 和目录访问，无需迁移；重新上传同样内容会生成新的 SHA-1 路径，不跨算法去重。

Code 沿用 infrago 的紧凑格式：`源名\t前缀\tkey\t扩展名\t大小`，再做无填充的
URL-safe Base64 编码。默认源名编码为空串，没有 JSON 字段名或版本前缀。
Hash 和 Version 仅保留在上传返回的 File 中，不进入 Code；因此 Decode 紧凑 Code
不会恢复这两个字段。兼容读取之前的 `v1.` / `v2.` JSON 引用，其中 v1 的 Version
解释为 Hash，保留旧 SHA-256 摘要；重新调用 Code() 会输出紧凑格式。

Code 是对象定位信息，不是鉴权凭据，也不会锁定历史版本。覆盖同一个 key 后，旧 Code
仍定位当前内容；需要当前元数据时用 Stat。同位置、同大小的对象 Code 不变，后端版本
变化不会影响 Code；大小变化仍会改变 Code。判断同一位置使用 Base + File。
读旧 Code 按其中的源定位，不重新经过哈希环；源名称应保持稳定，改名不会自动迁移旧引用。

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
- 本地 Browse 需要 `settings.base_url`，否则返回 ErrBrowseUnsupported。
- 本地驱动不支持非空 Metadata、Tags、Expires，明确返回 ErrUnsupported；不会静默忽略。
  Name / Mimetype 用于推断扩展名，本地驱动不额外保存原始文件名或 Content-Type。
- UploadOption.Expires 表示 HTTP 缓存过期元数据，不表示自动删除文件。
- BrowseOption.Expires 表示访问 URL 的有效期，Headers 表示签名绑定的请求头；本地
  静态 URL 不具备这些能力，会返回 ErrUnsupported。Params 仍可作为普通查询参数使用。
- 使用 errors.Is(err, storage.ErrUnsupported) 可统一识别不支持的能力或选项。

Fetch、Download、Local、Browse、Stat、Remove 的 Option 均可带 Context。
后台流支持 Context 取消；Runtime.Close 会取消操作、自动关闭未关闭的 Fetch 流，
等待在途操作退出后再关闭驱动。自定义驱动和 Reader 需要响应取消。

## 下载缓存和图片处理

Download、图片 Thumbnail 和 Preview 的缓存由主模块统一管理：

- 下载和图片文件都在写完并关闭后原子发布，失败不会留下可复用的半成品。
- 临时目录和各类缓存目录按需创建，启动时不要求它们存在或可写。配置的相对目录在启动时确定位置。
- Fetch、显式/随机 key 上传、Remove 不需要本地缓存目录；本地驱动的对象目录仍在启动时打开。
- 框架覆盖或删除对象时清理已打开的对应缓存；尚未打开的缓存在首次读取时检查后端版本。
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

单连接可以直接写在根部，表示 `default`：

```toml
[storage]
type = "file"
prefix = "uploads"

[storage.settings]
root = "store/objects"
key_mode = "hash"
temp = "store/temp/default"
base_url = "https://files.example.com"
```

多连接统一使用 `connections`，模块选项单独放在 `options`：

```toml
[storage.options]
download = "store/download"
thumbnail = "store/thumbnail"
preview = "store/preview"
cache_ttl = "10m"
max_image_pixels = 40000000
max_thumbnail_pixels = 16000000

[storage.connections.default]
type = "file"
prefix = "uploads"

[storage.connections.default.settings]
root = "store/objects"
key_mode = "hash"
temp = "store/temp/default"
base_url = "https://files.example.com"

[storage.connections.archive]
type = "file"
weight = -1

[storage.connections.archive.settings]
root = "/data/archive"
key_mode = "random"
temp = "/data/staging"
```

- `storage.options`：模块共用的下载/图片缓存目录、缓存时限、图片处理限制。
- `storage.connections.<name>`：连接的 type、weight、prefix、proxy、remote、settings。
- 连接的 `settings`：对应驱动的参数。本地驱动的 root、base_url、key_mode、temp 都在这里。
- 本地 key_mode 默认 hash，可选 random；单次 UploadOption.KeyMode 可覆盖，显式 Key 优先。
- 本地 temp 默认 `store/temp/<连接名>`，各连接独立配置且按需创建；外部驱动自行解释自己的 settings。
- 根部连接字段仅定义 default，不会成为其他连接的公共默认值。不要同时定义根部连接和 connections.default，重复名称会报错。
- 零配置仍自动使用默认本地连接；只有 options 时也保持这个行为。

由 dubi-go 读取 JSON/TOML/YAML 后下发。配置统一使用 `options`、`connections`、`settings`。
多连接只接受 `storage.connections.<name>`；旧的 sources、直接命名连接、option/setting、thumb 等写法不再支持。
storage 只读取自身配置节，不读取全局 `[file]`。本地驱动参数只接受 root、base_url、key_mode、temp。
对象根目录和下载/图片缓存目录应分开配置。

weight 默认 1，0 按 1 处理，-1 仅显式选择，使用与其他 dubi-ex 模块相同的一致性哈希。
多个本地连接默认具有独立根目录；调整连接配置不会自动搬迁已有对象。

Config / Configs 可通过 dubi.Register 注册值或指针，Scope=`storage:config`。
文件字段逐项覆盖代码默认值。Thumbnailer、Previewer 的 Scope 分别是
`storage:thumbnailer`、`storage:previewer`，代码注册受 Profile 筛选。
内置图片处理器随模块提供，不依赖 Profile 注册。

## 扩展驱动

子包在 init 中调用 `storage.RegisterDriver`，接口见 [driver.go](driver.go)。
必需操作只有 Open、Close、Upload、Fetch、Remove；Stat、Local、Browse、Health
分别通过可选接口提供。模块统一负责选源、前缀、编码、缓存、下载及生命周期。
驱动 Upload 必须读到 EOF、传播读取错误后才原子提交，返回正确的对象位置和实际大小；
其中 Size 不符会由输入流报告 ErrSizeMismatch。通过 Instance.NewFile 的可选第五个
参数 ObjectInfo{Hash: ..., Version: ...} 返回摘要和后端版本，框架不会重新计算或覆盖它们。Fetch
可直接返回网络流，无需为满足 Seek/ReadAt 而先完整落盘。

需要自动生成 key 的驱动实现 UploadPreparer。在 PrepareUpload 中解析最终 Key、Prefix
及可选 Size，并返回可完整读取内容的 io.ReadCloser；框架在操作结束时关闭它，驱动负责
清理自己的准备资源，但不能关闭调用方原始 Reader。保留显式 Key、Type 及传入的前缀，
允许追加目录层级。未实现此接口的驱动需要调用方提供 Key；框架不会代其生成摘要命名。
准备完成后，框架再按最终位置加锁、执行 Upload 并使缓存失效。

支持 Metadata / Tags / Expires 的驱动额外实现 UploadValidator；支持限时或绑定请求头
URL 的驱动实现 BrowseValidator。校验方法必须拒绝不能兑现的参数，校验阶段不读写对象。
UploadValidator 在准备命名前调用，收到已组合连接前缀的选项，但 Key 仍可能为空。
没有实现这些接口时，框架拒绝相应高级选项。校验失败不会消费上传内容。

当前仅内置本地文件驱动，没有包含 S3/MinIO SDK。默认文件 IO 使用 os.Root 限制
相对路径和符号链接的访问范围。驱动应支持并发调用，流的 Close 应能终止阻塞读取。移植设计授权见 [LICENSE.infrago](LICENSE.infrago)。

可运行示例：`go run ./examples/storage`。多连接配置见 [examples/storage/config.toml](../examples/storage/config.toml)，
运行 `DUBI_CONFIG=examples/storage/config.toml go run ./examples/storage` 可加载该配置。
