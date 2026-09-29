# 第 1 项设计评审稿

## 状态与顺序

用户已确认按第 1、2、3 项顺序实施。第 1 项验收后进入第 2 项，第 3 项以第 2 项后的性能结果为基线。

## 最小改动

### 按需读取库

- `LibraryRepository` 增加无 Roots 预载、无 HasTable 的基础读取（单库及列表）；原 `FindByID/List` 保留完整契约，事务调用和目录消费者不受影响。
- 纯基础字段消费者切换到基础读取；正常 HTTP 热路径可走共享缓存，扫描、文件路径操作、写后回读和事务内调用保持直接读取。
- 不缓存数据库错误，不把不存在等同数据库故障，不把缓存库信息当访问许可；返回数据不允许调用方修改共享对象。
- `FindCoverURL` 已经按需读取，不能因统一入口而增加查询或 Roots。

### 请求内用户复用

- `active_user.go` 两条 guard 继续实时读取 users，验证 TokenVersion、禁用和过期，再将已验证用户的只读快照写入标准请求 context。
- service 可见性计算仅在目标用户 ID 与快照 ID 一致时复用；管理员代查其他用户仍读取目标用户，不复用认证管理员的权限。
- 不改变 UserRepository 的通用读取语义，避免事务、写后回读及后台调用拿到请求前的快照；不跨请求缓存认证结果。

### 缓存与失效

- 库基础缓存复用已有 RuntimeCache 的存储/容量能力；可见性缓存维持已有短 TTL 上界，不新建通用缓存框架。
- 配置变更成功后失效：库基础字段及删除、用户 HideAdult、默认播放配置和允许库、adult.enabled/adult.library_ids、相关显示配置。事务操作在提交成功后失效，不在未提交的 repository.New(tx) 上发布结果。
- 防旧计算回填：读取时固定仓储实例 UUID 与代际组成的键；写入成功后原子换代，旧并发读取只能填旧键，后续请求不会读取旧键，不另建锁协议。
- RuntimeCache 当前 Redis 后面仍有本地 L1；仅 DeletePrefix 不等于多实例实时失效。三个仓库 Compose 模板均定义一个应用服务，未配置副本数，但这不能证明实际部署只有一个实例。实施时核实共享读写和失效传播边界；若不能覆盖全部实例，相关入口保持直接读取，不默默扩大权限陈旧窗口或增加分布式基础设施。
- 请求内复用与无 Roots 基础读取不依赖跨请求缓存，缓存不可用时保持直接读取和原错误处理。

## 已定位的修改责任

| 责任 | 文件/模块 |
| --- | --- |
| 库基础读取与完整读取分离 | internal/repository/library_repository.go |
| 请求内已验证用户、目标用户匹配 | internal/handler/active_user.go、internal/handler/visibility.go、internal/service/visibility.go 及小型 context helper |
| 缓存装配、失效与防旧回填 | internal/service/runtime_cache.go、emby_visibility.go、service_builder.go 及相关配置写入口 |
| HTTP/Emby 基础字段消费者 | emby_hongguo.go、emby_items_detail.go、emby_artwork.go、emby_movie_items.go、emby_search_items.go、库展示及对应 handler |
| 库直接写入与事务后失效 | media_library.go、media_library_roots.go、scanner_roots.go、scanner_scan.go、service_library_normalize.go、emby_movie_items.go |
| 权限写入失效 | profile.go、play_profile.go、telegram_bot_commands_core.go、admin_settings.go，以及最终清单中同类入口 |

代码核验显示原始 CodeGraph 同名 FindByID 调用数混入其他仓储，不能拿 166 当 Library 的完整消费数。实施前以生产 `Library.FindByID/List` 实际调用清单逐点核对；扫描和 NFO 文件编辑涉及路径边界，不能因只看到 Type 字段就判为可缓存。

## 不做与回滚

不缓存完整接口响应、登录校验、事务结果或扫描目录；不移除 Roots 表兼容判断的所有用途；不引入新数据库列或分布式协调基础设施。代码回滚后沿用原读取路径；新增缓存命名空间不会被旧版本读取，旧值按 TTL 自然过期。

## 实现核验

- 权限计算与 DisplayLibraries 直接 ListBasic，不叠加库缓存；库缓存仅用于展示数据。失效限同一仓储实例，多实例权限仍维持原 30 秒 TTL 上界，未声称分布式即时一致。
- 根目录写入不改变基础缓存包含的字段；syncLibraryPrimaryRoot 更新 libraries.path 时已通过 UpdateFields 失效。单独给根目录写入加失效无法修复同步失败，也没有需要失效的 Roots 缓存，因此不添加。
- handler/media.go 返回完整 Library（含 Roots），不是纯基础字段消费者；保留原方法。

## 证据

- library_repository.go:51/64：完整读取包含 HasTable 和 Roots 预载；:83 已有单字段读取。
- active_user.go:15/50：当前只注入 Gin ID/角色/等级和 Emby 用户名，未传播完整用户快照。
- emby_visibility.go:52 与 emby_compat.go:106：30 秒 map 缓存；无锁计算后回填，需代际保护。
- runtime_cache.go:79/100/115：内存 L1、可选 Redis、DeletePrefix；没有原子失效后回填保护。
- admin_settings.go:68：成人库配置首次启用还会批量更新 HideAdult，须覆盖其失效。
