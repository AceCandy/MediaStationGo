# 红果弹幕实时合并设计

## 最小行为差异与隔离边界

当前 raw 接口始终返回 200 空文本。此次只让已认证、具有目标文件可见性的红果分集进入新弹幕服务；普通媒体/NFO 仍走旧空响应。不把拉取逻辑接到播放记录、PlaybackInfo、视频流、收藏或标记已观看中。

| 交点 | 必要改动 | 保持不变 |
| --- | --- | --- |
| 模型注册 | 新增 `hongguo_danmus` 表及自身约束/索引 | 不改变旧表字段、触发器和历史数据迁移规则 |
| 外部 API | 新增 `hongguo` provider 分支 | 其他 provider 的更新、清除、代理和请求语义 |
| Emby raw | 增加红果分集的实际响应 | 路径、已有认证、非红果空响应 |
| 服务容器 | 注入新服务并等待其异步保存结束 | 下载/同步任务调度和原关闭顺序 |

禁止将新增模型放进资料清理枚举 `HongGuoModels()`：该枚举属于来源资料生命周期（`internal/model/hongguo.go:155`）。在 `AllModels()` 单独注册（`internal/model/model.go:48`），不设会级联删除旧资料或历史弹幕的关联。新表不参加媒体、收藏、观看、下载和搜索维护触发器。

## 数据表与合并契约

一条弹幕一行，表名 `hongguo_danmus`：

- `source_id`：上游作品字符串 ID，不是合集 ID。
- `episode_number`：正整数集号。
- `comment_id`：来源单条弹幕字符串 ID。
- `offset_ms`：非负毫秒播放位置。
- `content`：正文，使用 `text`。
- `source_created_at`：来源创建时间，不冒充更新时间。
- 必要的本地插入时间；不保存账号、设备、用户头像、源 HTTP 响应或视频签名地址。

使用 `(source_id, episode_number, comment_id)` 复合主键；按前两个字段限定读取。模型不需要额外 UUID 或分集视频 ID 外键。批量插入用 `ON CONFLICT DO NOTHING`、固定批次，禁止覆盖整集、删除缺失条目或更新旧条目正文。

先将历史按 ID 放入 map，再加入本次已验证的未知 ID，最后按 `(offset_ms, comment_id)` 稳定排序。已有 ID 保留历史内容。相同文本/时间但不同 ID 均保留；无有效 ID、非法时间或空正文的源记录不能伪造身份。异步只写本次新增部分，数据库唯一键兜住并发重复。

## 配置所有权与安全

复用 `/api/admin/api-configs/hongguo` 及既有管理员页面，不另建设置页面/路由。`APIConfigService.SeedDefaults` 仅补充缺失的红果 provider，不修改已存在的其他配置行。

在现有 `api_configs.api_key` 加密 text 列中保存红果专用结构化参数包，不为此增加另一张设置表或改变共享模型字段类型。新增类型化 patch `hongguo_app`；仅红果分支接受，不改变旧 `api_key` 的空字符串/省略语义。

表单：Cookie、x-tt-token、User-Agent、device_id、iid；高级区域提供 App 公共查询参数 JSON，用于版本、aid、app_name、设备类型等已支持字段。全部可空。只允许明确的键集合和字符串值，禁止自定义 endpoint/host、签名和时间戳；签名动态生成。不能用共享明文 `extra` 存储这些值。

未填写的字段不提交，保留已存值；清除按钮明确清除整组参数。PublicView 仅返回每项已配置状态，不返回明文、局部设备标识或加密包内容；对红果不使用通用首尾字符遮罩展示 JSON。普通保存及禁用不触发网络请求。配置缺失或全空为匿名模式；禁用停止上游拉取但仍可返回有权限的本地历史。

每次逻辑弹幕拉取解析一份当前配置快照，整个窗口遍历保持一致；下一次请求观察新配置。检查加密服务实际可用，不能接受现有 helper 的降级明文结果；解密/解析失败不把密文或无效凭据发往来源。接口校验错误只返回字段名和固定原因，不返回值。既有旧 `/api/api-config` 路由的模型 `APIKey json:"-"` 也要做泄漏回归。

## 读取链路

1. 现有 Emby middleware 验证用户。对红果逻辑分集 ID 使用 `hongGuoItemFiles` 的原生分集谓词和文件权限范围，只解析目标作品和集号，不展开展示层级/整季状态。若客户端使用文件 ID，先读取可见的红果绑定再解析同一坐标。合集/季/人物不能触发批量弹幕请求。
2. 按源作品＋集号读取历史。权限检查必须早于历史返回、上游请求和共享并发合并。不可见/不存在的红果分集为 404；非红果保持既有空占位。
3. 内部使用已有目标集的视频映射；缺少/明确失效时只读取目标作品详情按集号解析，不调用资料持久化或全季抓取。不修改旧分集表。
4. 新弹幕客户端向固定红果 App 主机发请求，读取该集真实时长后按时间窗口/cursor 遍历。不要假设统计数等于实际可返回条数，也不要把播放 cursor 当新增时间戳。
5. 将每个成功响应中通过校验的真实弹幕与历史合并。业务错误、超时或解析失败不删除历史；已完成窗口的有效条目仍可参与合并。两边都没有数据时返回合法空 XML。
6. 发布合并响应后异步插入新条目。配置为禁用时跳过 3–5 的上游部分，仅序列化历史。

新请求客户端不修改 `Client.appRequest` 或 `signDownloadAppRequest`（`internal/hongguo/download_app.go:40,96`）。旧下载/合集请求只有简化 Gorgon 签名，而调研中的弹幕使用 Argus/Gorgon/Khronos/Ladon；需新增弹幕专用实现并以固定向量校验，不能以旧下载成功证明新签名正确。优先复用标准库及已装依赖中的算法；若需新密码算法依赖，先核对许可证、版本和测试向量，不写未经验证的替代算法。

请求体保留已验证的 `comment_source=601`、`server_channel=1000`、`group_type=30`、`comment_type=20`、`count=90`、`sort=1`、`need_danmaku_guide_type=[]`；aid 与所用客户端查询参数一致。来源 ID 和 cursor 数值用字符串/`json.Number`，禁止 float64 丢失精度。固定来源、不随重定向发送凭据、响应体/页数/总时间有界；时间、cursor 不前进要停止并标识为未完整获取。

限制同时工作的目标集数量，窗口顺序拉取并限速，冷却不能超过本次截止时间。最多共享同一集正在进行的获取，不增加“缓存命中就跳过实时请求”的结果缓存。匿名身份仅在内存产生且同一遍历保持一致，不复制开源项目固定设备身份，不注册设备、不发送观看行为。

异步保存由新服务自己的生命周期管理，响应取消不能直接取消已接纳保存；进程关闭停止接纳并取消/等待，接入 `Container.Close`（`internal/service/service.go:149`）。将并发名额保持到该请求的保存结束，避免无界写协程；不创建调度任务、持久队列或新任务中心条目。失败只记录安全分类及计数，不记录内容、Cookie、Token 或源 URL。突然退出可能丢失尚未落库的增量，这是异步要求本身的边界。

## Emby 输出契约

权威参考：

- https://github.com/fengymi/emby-plugin-danmu/blob/HEAD/Emby.Plugin.Danmu/Core/Controllers/DanmuController.cs — `DanmuParams` 包含 `/api/danmu/{id}/raw`，默认 `DownloadXml`；`Download` 返回 XML 文件字节。
- https://github.com/fengymi/emby-plugin-danmu/blob/HEAD/Emby.Plugin.Danmu/Scraper/Entity/ScraperDanmaku.cs — XML root `i`、条目 `d`、属性 `p`，`WriteXml` 将毫秒除以 1000。
- https://github.com/huangxd-/danmu_api/blob/HEAD/danmu_api/sources/hongguo.js — 弹幕来源协议参考，不是 Emby raw 的 JSON 契约。

返回 `application/xml; charset=utf-8`，UTF-8 XML `<i>`，条目 `<d p="秒,模式,字号,颜色,创建时间,池,发送者占位,ID,权重">正文</d>`；保留源字符串 ID，不用浮点转换。普通红果文字采用普通滚动/白色，正文使用 `encoding/xml` 转义，处理 XML 非法控制字符，保留毫秒精度。可提供 `sourceprovider` 和实际 `datasize`。不返回源用户身份。设置 `Cache-Control: no-store`，避免 HTTP 缓存绕过实时获取。

本地插件当前默认方法直接返回字节，源码不能确定宿主最终 Content-Type；本服务显式输出 XML MIME，需 YAMBy 实机验证，不声称已经完成客户端兼容测试。保留无 `/emby` 和 `/emby` 两种已注册前缀，不扩展未请求的插件搜索/管理端点。补充静态 Emby API 目录中的实际支持范围（目前未发现该路由目录项）。

## 文件范围与验证

预计必要已有文件：模型注册 `internal/model/model.go`，配置服务 `internal/service/api_config.go`，服务注入/关闭 `service_builder.go`、`service.go`，空 handler `emby_static.go`，Web `APIConfigsPanel.tsx`、`api/api_configs.ts`、`pages/embyApiCatalog.ts`。若请求体安全边界需要调整，只对新红果分支改 `handler/api_config.go`。

新增文件只覆盖红果弹幕模型、持久化方法、上游客户端/签名、合并服务及其测试。不重构共享下载客户端、迁移框架或外部 API 组件体系。旧行为通过非红果空响应、旧配置 round-trip、App 下载/合集请求、收藏/已观看/播放状态及资料清理回归验证。

## 发布与回滚

部署仍需单独授权。本次只增加新表及一条 provider 配置，没有历史转换或启动回填；已有项目迁移仍由原入口负责，不重写它。回滚旧程序后新表和配置可以留存，不应自动删除；旧业务表无需逆向数据迁移。关闭红果弹幕配置只停止新获取，不删除积累的弹幕。真实源站全量、登录对照和 YAMBy 显示属于独立验收，不能以 mock 测试替代。
